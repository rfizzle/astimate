package model_test

import (
	"errors"
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/model"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/regress"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/synth"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// planted are the parameters the recovery tests plant: none of them is the
// base value the fit starts from.
func planted() model.Params {
	return model.Params{Overhead: 15000, Scale: 2.5, Budget: 25000,
		PerExport: 60, PerUntested: 500, PerHidden: 300, Exponent: 1.4}
}

// base is the starting point: the shipped default's 7.2 parameters.
func base() model.Params {
	return model.Params{Budget: 25000, PerExport: 40, PerUntested: 800, PerHidden: 400, Exponent: 1.3}
}

// observe returns one observation per package, the median of runs noisy
// draws of the planted model.
func observe(pkgs []metrics.RawMetrics, p model.Params, noise float64, runs int, seed uint64) []model.Obs {
	rng := rand.New(rand.NewPCG(seed, seed))
	obs := make([]model.Obs, len(pkgs))
	for i := range pkgs {
		want := p.Measured(model.InputsOf(&pkgs[i]))
		draws := make([]float64, runs)
		for r := range draws {
			draws[r] = want * (1 + noise*rng.NormFloat64())
		}
		obs[i] = model.Obs{Package: "p", Metrics: pkgs[i], Tokens: regress.Median(draws)}
	}
	return obs
}

func TestFitFormRecoversPlantedParameters(t *testing.T) {
	shipped, err := definition.LoadDefinition("../../../rebuild.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var experiments []metrics.RawMetrics
	for _, e := range shipped.Experiments {
		experiments = append(experiments, e.Metrics.RawMetrics)
	}
	tests := []struct {
		name  string
		pkgs  []metrics.RawMetrics
		noise float64
	}{
		{"exact, random packages", synth.RandomMetrics(60, 1), 0},
		{"5% noise, random packages", synth.RandomMetrics(200, 2), 0.05},
		{"exact, the 34 experiments", experiments, 0},
		{"5% noise, the 34 experiments", experiments, 0.05},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := planted()
			obs := observe(tt.pkgs, want, tt.noise, 3, 7)
			ff, err := model.FitForm(obs, base())
			if err != nil {
				t.Fatal(err)
			}
			if len(ff.Fixed) != 0 {
				t.Fatalf("fixed %v, want every parameter fitted", ff.Fixed)
			}
			got, se := ff.Params, ff.SE
			for _, c := range []struct {
				name          string
				got, se, want float64
			}{
				{"overhead", got.Overhead, se.Overhead, want.Overhead},
				{"scale", got.Scale, se.Scale, want.Scale},
				{"per export", got.PerExport, se.PerExport, want.PerExport},
				{"per untested", got.PerUntested, se.PerUntested, want.PerUntested},
				{"per hidden", got.PerHidden, se.PerHidden, want.PerHidden},
				{"exponent", got.Exponent, se.Exponent, want.Exponent},
			} {
				// Exact data must land on the planted value; noisy data
				// within four of the fit's own standard errors, which
				// checks the standard errors as well.
				tol := 1e-6 * math.Abs(c.want)
				if tt.noise > 0 {
					tol = 4 * c.se
				}
				if math.Abs(c.got-c.want) > tol {
					t.Errorf("%s = %.4g (SE %.2g), want %.4g within %.2g", c.name, c.got, c.se, c.want, tol)
				}
			}
			if got.Budget != 25000 {
				t.Errorf("budget = %v, want it held at 25000", got.Budget)
			}
			t.Logf("fit %+v, R2 %.4f, past knee %d", got, ff.Fit.R2, ff.PastKnee)
		})
	}
}

func TestFitFormHoldsWhatTheDataCannotFit(t *testing.T) {
	pkgs := synth.RandomMetrics(40, 3)
	for i := range pkgs {
		// Small packages and no hidden state: nothing reaches the knee and
		// the hidden-state cost has nothing to fit.
		pkgs[i].TokensEst = 100 + pkgs[i].TokensEst%2000
		pkgs[i].TokensEstWithTests = pkgs[i].TokensEst + 50
		pkgs[i].UntestedExports %= 4
		pkgs[i].Globals, pkgs[i].InitFuncs = 0, 0
	}
	p := planted()
	ff, err := model.FitForm(observe(pkgs, p, 0, 3, 1), base())
	if err != nil {
		t.Fatal(err)
	}
	if why := ff.Fixed[model.NameExponent]; !strings.Contains(why, "the exponent needs 3") || ff.Params.Exponent != 1.3 {
		t.Errorf("exponent %v fixed %v, want held at the base 1.3", ff.Params.Exponent, ff.Fixed)
	}
	if _, ok := ff.Fixed[model.NamePerHidden]; !ok || ff.Params.PerHidden != 400 {
		t.Errorf("per hidden %v fixed %v, want held at the base 400", ff.Params.PerHidden, ff.Fixed)
	}
	if math.Abs(ff.Params.PerExport-p.PerExport) > 1e-3*p.PerExport {
		t.Errorf("per export = %v, want %v", ff.Params.PerExport, p.PerExport)
	}
}

func TestFitFormTooFew(t *testing.T) {
	obs := observe(synth.RandomMetrics(4, 5), planted(), 0, 3, 1)
	var tooFew *model.TooFewError
	if _, err := model.FitForm(obs, base()); !errors.As(err, &tooFew) || tooFew.Packages != 4 || !strings.Contains(err.Error(), "needs at least") {
		t.Fatalf("err = %v, want a TooFewError", err)
	}
}

func TestParams(t *testing.T) {
	// The SPEC.md 7.2 worked example, with a scale of 1 and no overhead.
	m := metrics.RawMetrics{SLOC: 1200, TokensEst: 10000, TokensEstWithTests: 16000, DuplicationPct: 20,
		ExportedSymbols: 30, UntestedExports: 15, Globals: 2, InitFuncs: 1}
	p := base()
	p.Scale = 1
	in := model.InputsOf(&m)
	if got := p.RebuildTokens(in); got != 28400 {
		t.Errorf("rebuild tokens = %v, want 28400", got)
	}
	if got := p.Ratio(in); math.Abs(got-1.136) > 1e-12 {
		t.Errorf("ratio = %v, want 1.136", got)
	}
	want := score.Estimate(m, score.RebuildParams{ContextBudget: 25000, TokensPerExport: 40,
		TokensPerUntestedExport: 800, TokensPerHiddenState: 400, SuperlinearExponent: 1.3}).AgentPasses
	if got := p.Passes(in); math.Abs(got-want) > 1e-12 {
		t.Errorf("passes = %v, want score.Estimate's %v", got, want)
	}
	if got := p.Measured(in); math.Abs(got-25000*want) > 1e-6 {
		t.Errorf("measured = %v, want %v", got, 25000*want)
	}
	cov := 50.0
	m.CoveragePct = &cov
	if got := model.InputsOf(&m).Untested; got != 7.5 {
		t.Errorf("untested with 50%% coverage = %v, want 7.5", got)
	}
}

func TestProfile(t *testing.T) {
	obs := observe(synth.RandomMetrics(80, 4), planted(), 0, 3, 1)
	pts := model.Profile(obs, base(), []float64{12500, 25000, 50000})
	if len(pts) != 3 {
		t.Fatalf("%d points, want 3", len(pts))
	}
	for _, pt := range pts {
		if pt.Err != nil {
			t.Fatalf("budget %v: %v", pt.Budget, pt.Err)
		}
		if pt.Fit.Params.Budget != pt.Budget {
			t.Errorf("budget %v fitted at %v", pt.Budget, pt.Fit.Params.Budget)
		}
	}
	if best := pts[1].Fit.Fit.SSE; best > pts[0].Fit.Fit.SSE || best > pts[2].Fit.Fit.SSE {
		t.Errorf("SSE %v at the planted budget is not the smallest: %v, %v", best, pts[0].Fit.Fit.SSE, pts[2].Fit.Fit.SSE)
	}
}
