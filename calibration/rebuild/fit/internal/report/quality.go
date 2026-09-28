package report

import (
	"math"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/model"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/regress"
	"github.com/rfizzle/astimate/internal/score"
)

// tierResidual summarizes the form's residuals over the packages of one
// tier, the tier from the fitted parameters.
type tierResidual struct {
	tier score.Tier
	n    int
	// meanResidual is the mean of measured minus predicted tokens.
	meanResidual float64
	// meanAbsPct and maxAbsPct are the mean and largest absolute residual
	// as a percentage of the measured tokens.
	meanAbsPct, maxAbsPct float64
}

// residualsByTier groups the form's residuals on obs under p by tier.
func residualsByTier(obs []model.Obs, p *model.Params, tiers score.Tiers) []tierResidual {
	out := []tierResidual{{tier: score.TierOnePass}, {tier: score.TierFewPasses}, {tier: score.TierPartition}}
	for i := range obs {
		in := model.InputsOf(&obs[i].Metrics)
		t := score.TierOf(p.Passes(in), tiers)
		res := obs[i].Tokens - p.Measured(in)
		pct := 0.0
		if obs[i].Tokens != 0 {
			pct = math.Abs(res) / obs[i].Tokens * 100
		}
		for j := range out {
			if out[j].tier == t {
				out[j].n++
				out[j].meanResidual += res
				out[j].meanAbsPct += pct
				out[j].maxAbsPct = math.Max(out[j].maxAbsPct, pct)
			}
		}
	}
	for j := range out {
		if out[j].n > 0 {
			out[j].meanResidual /= float64(out[j].n)
			out[j].meanAbsPct /= float64(out[j].n)
		}
	}
	return out
}

// outputFit is a straight-line fit of one measured output on the fitted
// estimate; ok is false when it could not be made.
type outputFit struct {
	n                    int
	ok                   bool
	intercept, slope, r2 float64
}

// fitOutput fits y = a + b x by least squares.
func fitOutput(x, y []float64) outputFit {
	o := outputFit{n: len(x)}
	if len(x) < 3 {
		return o
	}
	rows := make([][]float64, len(x))
	for i := range x {
		rows[i] = []float64{1, x[i]}
	}
	f, err := regress.OLS(rows, y)
	if err != nil {
		return o
	}
	o.ok, o.intercept, o.slope, o.r2 = true, f.Coef[0], f.Coef[1], f.R2
	return o
}
