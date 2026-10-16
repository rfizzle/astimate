package narrate

import (
	"math"
	"strconv"

	"github.com/rfizzle/astimate/internal/report"
)

// HumanBasis says, in full, where the engineer time comes from under b, the
// configured model and its numbers. The glossary's "Rebuild cost" entry and
// the JSON report's human_basis field carry it.
func HumanBasis(b report.HumanBasis) string {
	const measured = " The agent sittings were measured on 85 rebuilds."
	if b.Model == "" {
		return "Engineer time is a textbook figure, not a measurement." + measured
	}
	s := "Engineer time is a textbook figure, not a measurement: " + b.Model + ", " + basisRate(b, "thousand lines")
	if b.PerPackage {
		s += " before each package's complexity rating"
	}
	s += ", " + decimal(b.DaysPerMonth) + " working days a month, inflated by up to half where exports have no tests, " +
		"summed over the packages." + measured
	if b.PerPackage {
		return s + " The ratings under rebuild.cocomo in the configuration set the engineer time."
	}
	return s + " rebuild.cocomo_a in the configuration scales the engineer time."
}

// HumanBasisShort is HumanBasis in one line, for the footnotes of the text
// and Markdown reports and the hero's tooltip.
func HumanBasisShort(b report.HumanBasis) string {
	const measured = "not a measurement; agent sittings are measured."
	if b.Model == "" {
		return "Engineer time is a textbook figure, " + measured
	}
	knob := " rebuild.cocomo_a scales it."
	if b.PerPackage {
		knob = " rebuild.cocomo rates it."
	}
	return "Engineer time is " + b.Model + " (" + basisRate(b, "1,000 lines") + "), " + measured + knob
}

// basisRate is b's coefficient per unit of lines, to the power of its
// exponent.
func basisRate(b report.HumanBasis, unit string) string {
	return decimal(b.Coefficient) + " person-months per " + unit + " to the power " + decimal(b.Exponent)
}

// decimal writes v to at most three decimals.
func decimal(v float64) string {
	return strconv.FormatFloat(math.Round(v*1000)/1000, 'f', -1, 64)
}
