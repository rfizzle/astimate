package narrate

import (
	"testing"

	"github.com/rfizzle/astimate/internal/report"
)

func TestHumanBasis(t *testing.T) {
	t.Parallel()

	profile := report.HumanBasis{Model: "COCOMO II, rebuild profile",
		Basic81:      report.ModelBasis{Name: "COCOMO 81", Coefficient: 2.4, Exponent: 1.05},
		COCOMOII:     report.ModelBasis{Name: "COCOMO II, rebuild profile", Coefficient: 2.3814, Exponent: 1.0528},
		DaysPerMonth: 19, UNFM: "completely_unfamiliar", Unfamiliarity: 1}
	basic := profile
	basic.Model = "COCOMO 81"
	basic.UNFM, basic.Unfamiliarity = "mostly_familiar", 0.2
	familiar := profile
	familiar.UNFM, familiar.Unfamiliarity = "completely_familiar", 0
	const models = "Engineer time is a range from two textbook models, not a measurement: for each package, the low end " +
		"is the lower and the high end the higher of COCOMO 81 (2.4 person-months per thousand lines to the power " +
		"1.05) and COCOMO II, rebuild profile (2.381 person-months per thousand lines to the power 1.053, before the " +
		"package's complexity rating), at 19 working days a month, on lines "
	const tail = " The agent sittings were measured on 85 rebuilds. The ratings under rebuild.cocomo in the " +
		"configuration set the COCOMO II, rebuild profile figure, and rebuild.cocomo_a scales the COCOMO 81 one."
	const short = "Engineer time runs, per package, from the lower to the higher of COCOMO 81 and COCOMO II, rebuild " +
		"profile; not a measurement; agent sittings are measured. rebuild.cocomo and rebuild.cocomo_a set it."
	tests := []struct {
		name        string
		b           report.HumanBasis
		long, short string
	}{
		{
			name: "rebuild profile",
			b:    profile,
			long: models + "inflated by COCOMO II's software understanding, 20% to 100% under unfm completely_unfamiliar, " +
				"from each package's tests, structure and doc comments, summed over the packages. The JSON field " +
				"human_days is the configured model's, COCOMO II, rebuild profile." + tail,
			short: short,
		},
		{
			name: "basic-81, mostly familiar",
			b:    basic,
			long: models + "inflated by COCOMO II's software understanding, 4% to 20% under unfm mostly_familiar, " +
				"from each package's tests, structure and doc comments, summed over the packages. The JSON field " +
				"human_days is the configured model's, COCOMO 81." + tail,
			short: short,
		},
		{
			name: "completely familiar",
			b:    familiar,
			long: models + "not inflated for understanding, unfm being completely_familiar, summed over the packages. " +
				"The JSON field human_days is the configured model's, COCOMO II, rebuild profile." + tail,
			short: short,
		},
		{
			name:  "unknown",
			long:  "Engineer time is a textbook figure, not a measurement. The agent sittings were measured on 85 rebuilds.",
			short: "Engineer time is a textbook figure, not a measurement; agent sittings are measured.",
		},
	}
	for _, tt := range tests {
		if got := HumanBasis(tt.b); got != tt.long {
			t.Errorf("%s: HumanBasis =\n%q\nwant\n%q", tt.name, got, tt.long)
		}
		if got := HumanBasisShort(tt.b); got != tt.short {
			t.Errorf("%s: HumanBasisShort =\n%q\nwant\n%q", tt.name, got, tt.short)
		}
	}

	// A rating that moves the coefficient moves the label.
	moved := profile
	moved.COCOMOII.Coefficient = 2.94
	if HumanBasis(moved) == HumanBasis(profile) {
		t.Error("the label did not follow the coefficient")
	}
}
