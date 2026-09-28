package model_test

import (
	"math"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/model"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/synth"
	"github.com/rfizzle/astimate/internal/metrics"
)

// TestStatisticalCalibration repeats a noisy synthetic experiment over many
// seeds and checks that the standard errors mean what the report says: the
// planted per-item costs and exponent fall within two standard errors about
// 95% of the time, and a candidate the planted cost does not read is called
// significant about 5% of the time.
func TestStatisticalCalibration(t *testing.T) {
	const trials = 200
	fp := map[string]int{}
	cover := map[string]int{}
	for s := range uint64(trials) {
		obs := observe(synth.RandomMetrics(60, 100+s), planted(), 0.03, 3, 500+s)
		ff, err := model.FitForm(obs, base())
		if err != nil {
			t.Fatal(err)
		}
		p := planted()
		for _, c := range []struct {
			name          string
			got, se, want float64
		}{
			{model.NamePerExport, ff.Params.PerExport, ff.SE.PerExport, p.PerExport},
			{model.NamePerUntested, ff.Params.PerUntested, ff.SE.PerUntested, p.PerUntested},
			{model.NamePerHidden, ff.Params.PerHidden, ff.SE.PerHidden, p.PerHidden},
			{model.NameExponent, ff.Params.Exponent, ff.SE.Exponent, p.Exponent},
		} {
			if math.Abs(c.got-c.want) <= 2*c.se {
				cover[c.name]++
			}
		}
		cs, _ := model.Contributions(obs, nil, model.Linearize(ff.Params))
		for _, c := range cs {
			if !c.InForm && c.CoefSignificant {
				fp[c.Input]++
			}
		}
	}
	t.Logf("within 2 SE, of %d: %v; candidates called significant: %v", trials, cover, fp)
	for name, n := range cover {
		if n < trials*85/100 {
			t.Errorf("%s within 2 SE in %d of %d trials, want at least 85%%", name, n, trials)
		}
	}
	if len(cover) != 4 {
		t.Errorf("coverage counted for %v, want 4 parameters", cover)
	}
	for name, n := range fp {
		if n > trials/10 {
			t.Errorf("%s called significant in %d of %d trials, want at most 10%%", name, n, trials)
		}
	}
}

func TestContributions(t *testing.T) {
	pkgs := synth.RandomMetrics(80, 9)
	// Plant a candidate: cognitive_p90 costs 2000 measured tokens a unit.
	obs := observe(pkgs, planted(), 0.01, 3, 3)
	for i := range obs {
		obs[i].Tokens += 2000 * float64(obs[i].Metrics.CognitiveP90)
	}
	runs := make([]model.Outcome, 0, len(pkgs))
	for i := range pkgs {
		// Passing depends on max_nesting alone.
		runs = append(runs, model.Outcome{Metrics: pkgs[i], Passed: pkgs[i].MaxNesting <= 3})
	}
	for i := range pkgs {
		// fan_in never varies.
		pkgs[i].FanIn = 2
		obs[i].Metrics.FanIn = 2
		runs[i].Metrics.FanIn = 2
	}
	ff, err := model.FitForm(obs, base())
	if err != nil {
		t.Fatal(err)
	}
	cs, lin := model.Contributions(obs, runs, model.Linearize(ff.Params))
	if lin == nil || !lin.Linearized || lin.N != 80 || lin.Col(model.InVolume) != 1 || lin.Col("nope") != -1 {
		t.Fatalf("linear fit %+v", lin)
	}
	got := map[string]model.Verdict{}
	for _, c := range cs {
		got[c.Input] = c.Verdict
	}
	for in, want := range map[string]model.Verdict{
		model.InVolume:       model.VerdictCost,
		model.InUntested:     model.VerdictCost,
		model.InCognitiveP90: model.VerdictCost,
		model.InMaxNesting:   model.VerdictPassOnly,
		model.InFanIn:        model.VerdictNoVariation,
	} {
		if got[in] != want {
			t.Errorf("%s: %q, want %q", in, got[in], want)
		}
	}
	if len(cs) != len(model.AllInputs()) {
		t.Errorf("%d contributions, want one per input", len(cs))
	}
}

func TestFitLinear(t *testing.T) {
	obs := observe(synth.RandomMetrics(50, 8), planted(), 0, 1, 1)
	for i := range obs {
		// Below the knee everywhere: exactly linear.
		p := planted()
		p.Budget = 1e9
		obs[i].Tokens = p.Measured(model.InputsOf(&obs[i].Metrics))
	}
	extra := model.AllInputs()[len(model.AllInputs())-1]
	lin, err := model.FitLinear(obs, &extra)
	if err != nil {
		t.Fatal(err)
	}
	if len(lin.Columns) != 7 || lin.Columns[6] != model.InFanIn {
		t.Fatalf("columns %v", lin.Columns)
	}
	p := planted()
	for name, want := range map[string]float64{
		"intercept": p.Overhead, model.InVolume: p.Scale, model.InSpec: p.Scale,
		model.InExports: p.Scale * p.PerExport, model.InUntested: p.Scale * p.PerUntested,
		model.InHidden: p.Scale * p.PerHidden, model.InFanIn: 0,
	} {
		if got := lin.Fit.Coef[lin.Col(name)]; math.Abs(got-want) > 1e-6*math.Max(1, want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
}

func TestContributionsWithoutAFit(t *testing.T) {
	obs := observe(synth.RandomMetrics(3, 1), planted(), 0, 1, 1)
	cs, lin := model.Contributions(obs, nil, nil)
	if lin != nil {
		t.Errorf("linear fit on 3 packages: %+v", lin)
	}
	for _, c := range cs {
		if c.CoefOK || c.Verdict != model.VerdictUntested {
			t.Errorf("%s: %+v, want no coefficient and no contribution", c.Input, c)
		}
	}
}

func TestLinearize(t *testing.T) {
	p := planted()
	undo := model.Linearize(p)
	for _, m := range synth.RandomMetrics(50, 2) {
		in := model.InputsOf(&m)
		want := p.Overhead + p.Scale*p.RebuildTokens(in)
		if got := undo(p.Measured(in)); math.Abs(got-want) > 1e-6*want {
			t.Fatalf("linearize(%v) = %v, want %v", p.Measured(in), got, want)
		}
	}
	if got := undo(100); got != 100 {
		t.Errorf("below the overhead: %v, want unchanged", got)
	}
}

func TestBestBudget(t *testing.T) {
	fit := func(sse float64, df int) model.FormFit {
		var f model.FormFit
		f.Fit.SSE, f.Fit.DF = sse, df
		return f
	}
	tests := []struct {
		name string
		pts  []model.ProfilePoint
		best float64
		sig  bool
	}{
		{"current is best", []model.ProfilePoint{{Budget: 1, Fit: fit(10, 30)}, {Budget: 2, Fit: fit(5, 30)}}, 2, false},
		{"better within noise", []model.ProfilePoint{{Budget: 1, Fit: fit(10, 30)}, {Budget: 2, Fit: fit(10.5, 30)}}, 1, false},
		{"better beyond noise", []model.ProfilePoint{{Budget: 1, Fit: fit(5, 30)}, {Budget: 2, Fit: fit(10, 30)}}, 1, true},
		{"failed points", []model.ProfilePoint{{Budget: 1, Err: &model.TooFewError{Packages: 1, Params: 2}}}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			best, sig := model.BestBudget(tt.pts, 2)
			if best != tt.best || sig != tt.sig {
				t.Errorf("BestBudget = %v, %v; want %v, %v", best, sig, tt.best, tt.sig)
			}
		})
	}
}

func TestAllInputs(t *testing.T) {
	m := metrics.RawMetrics{TokensEst: 1000, TokensEstWithTests: 1500, DuplicationPct: 10, ExportedSymbols: 7,
		UntestedExports: 3, Globals: 2, InitFuncs: 1, TestFuncs: 4, CognitiveTotal: 20, CognitiveP90: 5,
		MaxNesting: 3, FuncCount: 9, FanIn: 6}
	want := map[string]float64{
		model.InVolume: 900, model.InSpec: 500, model.InExports: 7, model.InUntested: 3, model.InHidden: 3,
		model.InTestFuncs: 4, model.InCognitiveTotal: 20, model.InCognitiveP90: 5, model.InMaxNesting: 3,
		model.InFuncCount: 9, model.InFanIn: 6,
	}
	forms := 0
	for _, in := range model.AllInputs() {
		if got := in.Value(&m); got != want[in.Name] {
			t.Errorf("%s = %v, want %v", in.Name, got, want[in.Name])
		}
		if in.InForm {
			forms++
		}
	}
	if forms != 5 {
		t.Errorf("%d form inputs, want 5", forms)
	}
}
