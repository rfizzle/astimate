package report

import "github.com/rfizzle/astimate/internal/score"

// HumanBasis is what the human estimate is computed with (SPEC.md 7.3): the
// two models the range runs between, the configured one, and the numbers the
// labels that say where the engineer time comes from print
// (narrate.HumanBasis).
type HumanBasis struct {
	// Model names the configured model, whose figure human_days is, as the
	// reports do (score.Cocomo.Name); empty when unknown.
	Model string
	// Basic81 and COCOMOII are the two models, COCOMO 81 and COCOMO II
	// under the rebuild profile: each package's range runs from the lower
	// of their figures to the higher.
	Basic81, COCOMOII ModelBasis
	// DaysPerMonth converts person-months into working days.
	DaysPerMonth float64
	// UNFM is rebuild.cocomo.unfm, the unfamiliarity rating, and
	// Unfamiliarity its value, 0 to 1: the software-understanding
	// inflation of the lines both models read is 20% to 100% times it.
	UNFM          string
	Unfamiliarity float64
}

// ModelBasis is one model's numbers.
type ModelBasis struct {
	// Name names the model as the reports do, score.ModelName.
	Name string
	// Coefficient is person-months per thousand lines before the exponent
	// (and, under COCOMO II, before each package's complexity rating).
	Coefficient float64
	// Exponent is the power on the thousand lines.
	Exponent float64
}

// NewHumanBasis returns the basis of the human estimate under p.
func NewHumanBasis(p score.RebuildParams) HumanBasis {
	c := p.Cocomo
	return HumanBasis{
		Model:         c.Name(),
		Basic81:       ModelBasis{Name: score.ModelName(score.ModelBasic81), Coefficient: p.CocomoA, Exponent: p.CocomoB},
		COCOMOII:      ModelBasis{Name: score.ModelName(score.ModelCOCOMOII), Coefficient: c.Coefficient(), Exponent: c.Exponent()},
		DaysPerMonth:  p.DaysPerMonth,
		UNFM:          c.UNFM,
		Unfamiliarity: c.Unfamiliarity(),
	}
}
