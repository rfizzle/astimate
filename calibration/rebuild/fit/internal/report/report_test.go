package report_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/dataset"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/emit"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/model"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/regress"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/report"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/synth"
	"github.com/rfizzle/astimate/internal/config"
)

// input fits a synthetic experiment and returns the report's input.
func input(t *testing.T, pkgs, failEvery int) *report.Input {
	t.Helper()
	rows := synth.Rows(synth.RandomMetrics(pkgs, 3), &synth.Plant{
		Params: model.Params{Overhead: 10000, Scale: 2, Budget: 25000, PerExport: 60, PerUntested: 500, PerHidden: 300, Exponent: 1.4},
		Noise:  0.02, Runs: 2, FailEvery: failEvery, Agent: "a", Model: "m", Seed: 5,
	})
	set, err := dataset.Build(rows, dataset.Footprint)
	if err != nil {
		t.Fatal(err)
	}
	def, err := config.Load("../../../../../configs/uncalibrated.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var obs []model.Obs
	for _, p := range set.Measured() {
		obs = append(obs, model.Obs{Package: p.Package, Metrics: p.Metrics, Tokens: regress.Median(p.Tokens)})
	}
	rp := def.Rebuild
	start := model.Params{Budget: rp.ContextBudget, PerExport: rp.TokensPerExport, PerUntested: rp.TokensPerUntestedExport,
		PerHidden: rp.TokensPerHiddenState, Exponent: rp.SuperlinearExponent}
	in := &report.Input{Version: "rebuild-2026-09-28-a", BaseVersion: def.Version, Runs: []string{"runs.jsonl"},
		Out: "c.yaml", Command: "go run ./calibration/rebuild/fit --runs runs.jsonl", Set: &set, Obs: obs, Base: rp}
	ff, err := model.FitForm(obs, start)
	if err != nil {
		in.FormErr = err
		in.Emitted = emit.FromFit(rp, nil, err.Error())
		in.Contributions, in.Linear = model.Contributions(obs, set.Outcomes, nil)
		return in
	}
	in.Form = &ff
	in.Emitted = emit.FromFit(rp, &ff, "")
	in.Contributions, in.Linear = model.Contributions(obs, set.Outcomes, model.Linearize(ff.Params))
	in.Profile = model.Profile(obs, start, []float64{12500, 25000, 50000})
	return in
}

// flat is s with every run of white space as one space.
func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

func TestRenderFitted(t *testing.T) {
	r := flat(report.Render(input(t, 50, 5)))
	for _, want := range []string{
		"# Rebuild parameters rebuild-2026-09-28-a",
		"100 runs of 50 packages by a (model m)",
		"20 runs failed and are censored",
		"The 7.2 form explains",
		"Changed: tokens_per_export 40 →",
		"tests count as cost, consistent with 7.2",
		"## Parameters", "## Data", "## Fit quality per output", "### Residuals by tier", "### Pass rate by tier",
		"## What each input contributes", "## Does the data fit the 7.2 form?", "with the fitted knee undone",
		"## Context budget", "## Packages", "## Censored runs", "## Excluded rows", "None.",
		"| ONE_PASS |", "| `cognitive_p90` |", "## Reproduce",
		"ASTIMATE_CONFIG=$PWD/c.yaml go test ./internal/invariants",
	} {
		if !strings.Contains(r, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}

func TestRenderUnfitted(t *testing.T) {
	in := input(t, 3, 0)
	if in.Form != nil {
		t.Fatal("three packages fitted")
	}
	r := flat(report.Render(in))
	for _, want := range []string{
		"could not be fitted", "must not ship", "No fit:", "The data cannot say whether tests count",
		"The free linear fit could not be made", "None: every valid run passed.",
		"`tokens_per_export`: kept at 40",
	} {
		if !strings.Contains(r, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}

func TestRenderSpecVerdicts(t *testing.T) {
	lin := func(spec, specSE float64) *model.LinearFit {
		return &model.LinearFit{
			Columns: []string{"intercept", model.InVolume, model.InSpec},
			N:       40,
			Fit: regress.Fit{Coef: []float64{0, 2, spec}, SE: []float64{1, 0.01, specSE}, DF: 37,
				Cov: [][]float64{{1, 0, 0}, {0, 1e-4, 0}, {0, 0, specSE * specSE}}},
		}
	}
	tests := []struct {
		name string
		lin  *model.LinearFit
		want string
	}{
		{"help", lin(-1, 0.1), "tests help, lowering the cost; 7.2's rate of 1 is outside the interval"},
		{"no cost", lin(0.1, 1), "tests carry no measurable cost, and the data cannot tell cost from help."},
		{"no cost, below 1", lin(0.2, 0.5), "tests carry no measurable cost, and the data cannot tell cost from help; " +
			"7.2's rate of 1 is outside the interval"},
		{"other rate", lin(6, 0.1), "tests count as cost; 7.2's rate of 1 is outside the interval"},
		{"as 7.2", lin(2, 0.1), "tests count as cost, consistent with 7.2's rate of 1"},
		// The spec coefficient alone is not significant (t = 1.67), but its
		// covariance with volume narrows the ratio's interval to about 0.2
		// to 0.8, which excludes both zero and 1.
		{"interval excludes zero", &model.LinearFit{
			Columns: []string{"intercept", model.InVolume, model.InSpec},
			N:       40,
			Fit: regress.Fit{Coef: []float64{0, 2, 1}, SE: []float64{1, math.Sqrt(0.5), 0.6}, DF: 37,
				Cov: [][]float64{{1, 0, 0}, {0, 0.5, 0.4}, {0, 0.4, 0.36}}},
		}, "tests count as cost; 7.2's rate of 1 is outside the interval"},
		{"no spec column", &model.LinearFit{Columns: []string{"intercept", model.InVolume}}, "spec or volume does not vary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := input(t, 40, 0)
			in.Linear = tt.lin
			if tt.lin.Fit.Coef == nil {
				in.Linear.Fit = regress.Fit{Coef: []float64{0, 1}, SE: []float64{1, 1}, DF: 38,
					Cov: [][]float64{{1, 0}, {0, 1}}}
			}
			if r := flat(report.Render(in)); !strings.Contains(r, tt.want) {
				t.Errorf("report lacks %q", tt.want)
			}
		})
	}
}

func TestRenderNotesAndUnmeasured(t *testing.T) {
	in := input(t, 40, 0)
	in.Emitted.Notes = append(in.Emitted.Notes, emit.Note{Param: model.NamePerHidden, Text: "fitted -5, clamped to 0"})
	in.Set.Packages[0].Passes = 0
	in.Set.Excluded = append(in.Set.Excluded, dataset.Excluded{Package: "example.com/x", Run: 1, Reason: "the oracle did not complete"})
	in.Profile = []model.ProfilePoint{{Budget: 1, Err: errors.New("boom")}}
	r := flat(report.Render(in))
	for _, want := range []string{
		"tokens_per_hidden_state fitted -5, clamped to 0.",
		"1 packages never passed and are not in the fit at all",
		"Packages with no passing run, so no measurement at all:",
		"| `x` | 1 | the oracle did not complete |",
		"| 1 | - | - | - | boom |",
		"could not be refitted at other budgets",
	} {
		if !strings.Contains(r, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}
