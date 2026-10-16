package report

import "github.com/rfizzle/astimate/internal/score"

// HumanBasis is what the human estimate is computed with (SPEC.md 7.3): the
// model's name and the numbers the labels that say where the engineer time
// comes from print (narrate.HumanBasis).
type HumanBasis struct {
	// Model names the model as the reports do, score.Cocomo.Name; empty
	// when unknown.
	Model string
	// Coefficient is person-months per thousand lines before the exponent.
	Coefficient float64
	// Exponent is the power on the thousand lines.
	Exponent float64
	// DaysPerMonth converts person-months into working days.
	DaysPerMonth float64
	// PerPackage reports that each package's complexity adjusts the
	// coefficient, COCOMO II's CPLX.
	PerPackage bool
}

// NewHumanBasis returns the basis of the human estimate under p.
func NewHumanBasis(p score.RebuildParams) HumanBasis {
	c := p.Cocomo
	b := HumanBasis{Model: c.Name(), DaysPerMonth: p.DaysPerMonth}
	if c.Model == score.ModelBasic81 {
		b.Coefficient, b.Exponent = p.CocomoA, p.CocomoB
		return b
	}
	b.Coefficient, b.Exponent, b.PerPackage = c.Coefficient(), c.Exponent(), true
	return b
}
