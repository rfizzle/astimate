package report

import (
	"math"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/model"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/synth"
	"github.com/rfizzle/astimate/internal/score"
)

func TestResidualsByTier(t *testing.T) {
	p := model.Params{Overhead: 1000, Scale: 2, Budget: 25000, PerExport: 40, PerUntested: 800, PerHidden: 400, Exponent: 1.3}
	pkgs := synth.RandomMetrics(40, 6)
	obs := make([]model.Obs, len(pkgs))
	for i := range pkgs {
		obs[i] = model.Obs{Metrics: pkgs[i], Tokens: p.Measured(model.InputsOf(&pkgs[i]))}
	}
	obs[0].Tokens *= 1.1
	rs := residualsByTier(obs, &p, score.Tiers{OnePassMax: 1, FewPassesMax: 3})
	n, worst := 0, 0.0
	for _, r := range rs {
		n += r.n
		worst = math.Max(worst, r.maxAbsPct)
	}
	if n != 40 || len(rs) != 3 {
		t.Fatalf("%d packages in %d tiers, want 40 in 3", n, len(rs))
	}
	if math.Abs(worst-100*0.1/1.1) > 1e-9 {
		t.Errorf("largest residual %v%%, want %v%%", worst, 100*0.1/1.1)
	}
}

func TestFitOutput(t *testing.T) {
	x, y := []float64{1, 2, 3, 4}, []float64{3, 5, 7, 9}
	o := fitOutput(x, y)
	if !o.ok || math.Abs(o.slope-2) > 1e-9 || math.Abs(o.intercept-1) > 1e-9 || math.Abs(o.r2-1) > 1e-9 {
		t.Errorf("fitOutput = %+v, want y = 1 + 2x exactly", o)
	}
	if fitOutput(x[:2], y[:2]).ok {
		t.Error("a line through two points reported as a fit")
	}
}
