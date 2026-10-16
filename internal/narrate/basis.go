package narrate

import (
	"math"
	"strconv"

	"github.com/rfizzle/astimate/internal/report"
)

// HumanBasis says, in full, where the engineer time comes from under b: the
// two models each package's range runs between with their numbers, the
// software-understanding inflation under the configured unfamiliarity, and
// which model human_days is. The glossary's "Rebuild cost" entry and the
// JSON report's human_basis field carry it.
func HumanBasis(b report.HumanBasis) string {
	const measured = " The agent sittings were measured on 85 rebuilds."
	if b.Model == "" {
		return "Engineer time is a textbook figure, not a measurement." + measured
	}
	return "Engineer time is a range from two textbook models, not a measurement: for each package, the low end is " +
		"the lower and the high end the higher of " + b.Basic81.Name + " (" + basisRate(b.Basic81, "thousand lines") +
		") and " + b.COCOMOII.Name + " (" + basisRate(b.COCOMOII, "thousand lines") + ", before the package's " +
		"complexity rating), at " + decimal(b.DaysPerMonth) + " working days a month, on lines " + inflation(b) +
		", summed over the packages. The JSON field human_days is the configured model's, " + b.Model + "." + measured +
		" The ratings under rebuild.cocomo in the configuration set the " + b.COCOMOII.Name + " figure, and " +
		"rebuild.cocomo_a scales the " + b.Basic81.Name + " one."
}

// HumanBasisShort is HumanBasis in one line, for the footnotes of the text
// and Markdown reports and the hero's tooltip.
func HumanBasisShort(b report.HumanBasis) string {
	const measured = "not a measurement; agent sittings are measured."
	if b.Model == "" {
		return "Engineer time is a textbook figure, " + measured
	}
	return "Engineer time runs, per package, from the lower to the higher of " + b.Basic81.Name + " and " +
		b.COCOMOII.Name + "; " + measured + " rebuild.cocomo and rebuild.cocomo_a set it."
}

// inflation says how COCOMO II's software understanding inflates the lines
// under b's unfamiliarity: 0.02 × SU × UNFM, SU 10 to 50.
func inflation(b report.HumanBasis) string {
	if b.Unfamiliarity == 0 {
		return "not inflated for understanding, unfm being " + b.UNFM
	}
	return "inflated by COCOMO II's software understanding, " + decimal(20*b.Unfamiliarity) + "% to " +
		decimal(100*b.Unfamiliarity) + "% under unfm " + b.UNFM + ", from each package's tests, structure and doc comments"
}

// basisRate is m's coefficient per unit of lines, to the power of its
// exponent.
func basisRate(m report.ModelBasis, unit string) string {
	return decimal(m.Coefficient) + " person-months per " + unit + " to the power " + decimal(m.Exponent)
}

// decimal writes v to at most three decimals.
func decimal(v float64) string {
	return strconv.FormatFloat(math.Round(v*1000)/1000, 'f', -1, 64)
}
