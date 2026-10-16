package narrate

import (
	"testing"

	"github.com/rfizzle/astimate/internal/report"
)

func TestHumanBasis(t *testing.T) {
	t.Parallel()

	profile := report.HumanBasis{Model: "COCOMO II, rebuild profile", Coefficient: 2.3814, Exponent: 1.0528,
		DaysPerMonth: 19, PerPackage: true}
	basic := report.HumanBasis{Model: "COCOMO 81", Coefficient: 2.4, Exponent: 1.05, DaysPerMonth: 19}
	tests := []struct {
		name        string
		b           report.HumanBasis
		long, short string
	}{
		{
			name: "rebuild profile",
			b:    profile,
			long: "Engineer time is a textbook figure, not a measurement: COCOMO II, rebuild profile, 2.381 person-months " +
				"per thousand lines to the power 1.053 before each package's complexity rating, 19 working days " +
				"a month, inflated by up to half where exports have no tests, summed over the packages. The agent sittings " +
				"were measured on 85 rebuilds. The ratings under rebuild.cocomo in the configuration set the engineer time.",
			short: "Engineer time is COCOMO II, rebuild profile (2.381 person-months per 1,000 lines to the power 1.053), " +
				"not a measurement; agent sittings are measured. rebuild.cocomo rates it.",
		},
		{
			name: "basic-81",
			b:    basic,
			long: "Engineer time is a textbook figure, not a measurement: COCOMO 81, 2.4 person-months per thousand lines " +
				"to the power 1.05, 19 working days a month, inflated by up to half where exports have no tests, summed " +
				"over the packages. The agent sittings were measured on 85 rebuilds. rebuild.cocomo_a in the " +
				"configuration scales the engineer time.",
			short: "Engineer time is COCOMO 81 (2.4 person-months per 1,000 lines to the power 1.05), not a measurement; " +
				"agent sittings are measured. rebuild.cocomo_a scales it.",
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
	moved.Coefficient = 2.94
	if HumanBasis(moved) == HumanBasis(profile) || HumanBasisShort(moved) == HumanBasisShort(profile) {
		t.Error("the label did not follow the coefficient")
	}
}
