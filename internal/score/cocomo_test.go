package score

import (
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// rated returns a Cocomo of model with the drivers of kv, key and rating
// pairs, rated; the rest are nominal.
func rated(model string, kv ...string) Cocomo {
	c := Cocomo{Model: model}
	for i := 0; i+1 < len(kv); i += 2 {
		if !c.Set(kv[i], Rating(kv[i+1])) {
			panic("unknown driver " + kv[i])
		}
	}
	return c
}

// rebuildProfile is the shipped rebuild profile of rebuild.cocomo.
func rebuildProfile() Cocomo {
	return rated(ModelCOCOMOII, "prec", "very_high", "flex", "very_low", "resl", "extra_high",
		"team", "nominal", "pmat", "nominal", "docu", "very_low")
}

func TestCocomoNominal(t *testing.T) {
	t.Parallel()

	// Every rating nominal: ΣSF = 3.72 + 3.04 + 4.24 + 3.29 + 4.68 = 18.97,
	// E = 0.91 + 0.1897 = 1.0997, and every multiplier is 1, so A' = A.
	var c Cocomo
	approx(t, "A", c.Coefficient(), 2.94, 1e-12)
	approx(t, "E", c.Exponent(), 1.0997, 1e-12)
	c = rated("", "prec", "nominal", "flex", "nominal", "resl", "nominal", "team", "nominal", "pmat", "nominal",
		"docu", "nominal", "sced", "nominal")
	approx(t, "A named nominal", c.Coefficient(), 2.94, 1e-12)
	approx(t, "E named nominal", c.Exponent(), 1.0997, 1e-12)
}

func TestCocomoRebuildProfile(t *testing.T) {
	t.Parallel()

	// ΣSF = 1.24 + 5.07 + 0 + 3.29 + 4.68 = 14.28, E = 1.0528; DOCU very
	// low is 0.81, so A' = 2.94 * 0.81 = 2.3814.
	c := rebuildProfile()
	approx(t, "E", c.Exponent(), 1.0528, 1e-12)
	approx(t, "A'", c.Coefficient(), 2.3814, 1e-12)
	if got := c.Name(); got != "COCOMO II, rebuild profile" {
		t.Errorf("Name() = %q", got)
	}
}

// TestHumanDaysCOCOMOII checks the form, PM = A' * KSLOC^E * CPLX with RELY
// nominal, and days = PM * 19, against hand-computed cases from the
// published tables.
func TestHumanDaysCOCOMOII(t *testing.T) {
	t.Parallel()

	allVeryLow := rated("", "prec", "very_low", "flex", "very_low", "resl", "very_low", "team", "very_low",
		"pmat", "very_low")
	allVeryLow.UNFM = "completely_familiar"
	// familiar leaves kloc_eff the non-duplicated KLOC, UNFM 0, so the
	// published values show through.
	familiar := Cocomo{UNFM: "completely_familiar"}
	tests := []struct {
		name string
		c    Cocomo
		m    metrics.RawMetrics
		want float64
	}{
		{
			// 2.94 * 1^1.0997 * 1 = 2.94 PM; * 19 = 55.86.
			name: "nominal, 1 KSLOC",
			c:    familiar,
			m:    metrics.RawMetrics{SLOC: 1000, HasTests: true},
			want: 55.86,
		},
		{
			// 2.94 * 10^1.0997 = 36.9868 PM; * 19 = 702.7501.
			name: "nominal, 10 KSLOC, p90 8",
			c:    familiar,
			m:    metrics.RawMetrics{SLOC: 10000, HasTests: true, FuncCount: 40, CognitiveP90: 8},
			want: 702.7501,
		},
		{
			// SPEC.md 7.3's worked example: SU nominal (structure very
			// high, clarity low at 15 of 30 untested, self-description
			// nominal unmeasured; (4 + 1 + 2) / 3 rounds down to 2), 30;
			// kloc_eff = 0.96 * (1 + 0.02 * 30 * 1) = 1.536; CPLX nominal
			// (no functions measured).
			// 2.3814 * 1.536^1.0528 = 3.7417 PM; * 19 = 71.0917.
			name: "rebuild profile, SPEC.md worked example",
			c:    rebuildProfile(),
			m: metrics.RawMetrics{SLOC: 1200, DuplicationPct: 20, ExportedSymbols: 30, UntestedExports: 15,
				HasTests: true},
			want: 71.0917,
		},
		{
			// SU low ((2 + 1 + 2) / 3 = 1), 40: kloc_eff = 0.96 * 1.8 =
			// 1.728; CPLX high (p90 12) 1.17.
			// 2.3814 * 1.728^1.0528 * 1.17 = 4.9557 PM; * 19 = 94.1581.
			name: "rebuild profile, complex",
			c:    rebuildProfile(),
			m: metrics.RawMetrics{SLOC: 1200, DuplicationPct: 20, ExportedSymbols: 30, UntestedExports: 15,
				HasTests: true, FuncCount: 10, CognitiveP90: 12},
			want: 94.1581,
		},
		{
			// SU very low ((0 + 0 + 2) / 3 = 0), 50: kloc_eff = 2 * 2 = 4;
			// CPLX extra high (p90 45) 1.74.
			// 2.3814 * 4^1.0528 * 1.74 = 17.8332 PM; * 19 = 338.8316.
			name: "rebuild profile, untested and complex",
			c:    rebuildProfile(),
			m:    metrics.RawMetrics{SLOC: 2000, ExportedSymbols: 4, UntestedExports: 4, FuncCount: 5, CognitiveP90: 45},
			want: 338.8316,
		},
		{
			// ΣSF = 6.20 + 5.07 + 7.07 + 5.48 + 7.80 = 31.62, E = 1.2262;
			// CPLX very low (p90 1) 0.73.
			// 2.94 * 2^1.2262 * 0.73 = 5.0210 PM; * 19 = 95.3996.
			name: "scale factors very low",
			c:    allVeryLow,
			m:    metrics.RawMetrics{SLOC: 2000, HasTests: true, FuncCount: 3, CognitiveP90: 1},
			want: 95.3996,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := validParams()
			p.Cocomo = tt.c
			p.Cocomo.Model = ModelCOCOMOII
			approx(t, "human_days", Estimate(tt.m, p).HumanDays, tt.want, 1e-4)
		})
	}
}

func TestHumanDaysModelSwitch(t *testing.T) {
	t.Parallel()

	m := metrics.RawMetrics{SLOC: 1200, DuplicationPct: 20, ExportedSymbols: 30, UntestedExports: 15, HasTests: true}
	p := validParams()
	p.Cocomo = rebuildProfile()
	keep := 0.8
	approx(t, "cocomo-ii", Estimate(m, p).HumanDays, HumanDaysCOCOMOII(&m, p, keep), 1e-12)
	p.Cocomo.Model = ""
	approx(t, "empty model", Estimate(m, p).HumanDays, HumanDaysCOCOMOII(&m, p, keep), 1e-12)
	p.Cocomo.Model = ModelBasic81
	approx(t, "basic-81", Estimate(m, p).HumanDays, 71.5609, 1e-4)
	approx(t, "basic-81 seam", HumanDaysBasic81(&m, p, keep), 71.5609, 1e-4)
}

func TestHumanDaysFollowRatings(t *testing.T) {
	t.Parallel()

	m := metrics.RawMetrics{SLOC: 3000, HasTests: true}
	p := validParams()
	p.Cocomo = rebuildProfile()
	base := Estimate(m, p).HumanDays
	for _, mutate := range []func(*Cocomo){
		func(c *Cocomo) { c.Set("prec", ratingNominal) },
		func(c *Cocomo) { c.Set("docu", ratingNominal) },
		func(c *Cocomo) { c.Set("acap", ratingVeryHigh) },
	} {
		q := p
		mutate(&q.Cocomo)
		if got := Estimate(m, q).HumanDays; got == base {
			t.Errorf("rating change left human_days at %v", base)
		}
	}
}

func TestCocomoSet(t *testing.T) {
	t.Parallel()

	var c Cocomo
	if c.Set("rely", ratingHigh) || c.Set("unfm", ratingHigh) || c != (Cocomo{}) {
		t.Errorf("Set accepted a driver rebuild.cocomo does not rate: %+v", c)
	}
	if got := len(CocomoKeys()); got != configured {
		t.Errorf("CocomoKeys has %d drivers, want %d", got, configured)
	}
}

func TestCplxRating(t *testing.T) {
	t.Parallel()

	tests := []struct {
		funcs, p90 int
		want       Rating
	}{
		{0, 0, ratingNominal}, {3, 0, ratingVeryLow}, {3, 1, ratingVeryLow}, {3, 2, ratingLow}, {3, 4, ratingLow},
		{3, 5, ratingNominal}, {3, 10, ratingNominal}, {3, 11, ratingHigh}, {3, 20, ratingHigh},
		{3, 21, ratingVeryHigh}, {3, 40, ratingVeryHigh}, {3, 41, ratingExtraHigh},
	}
	for _, tt := range tests {
		if got := cplxRating(&metrics.RawMetrics{FuncCount: tt.funcs, CognitiveP90: tt.p90}); got != tt.want {
			t.Errorf("cplxRating(func_count %d, cognitive_p90 %d) = %s, want %s", tt.funcs, tt.p90, got, tt.want)
		}
	}
}

// TestRelyNominal checks that RELY stays nominal: neither tests nor
// coverage move the COCOMO II figure, so adding tests never raises it.
func TestRelyNominal(t *testing.T) {
	t.Parallel()

	p := validParams()
	p.Cocomo = rebuildProfile()
	base := metrics.RawMetrics{SLOC: 1500, FuncCount: 8, CognitiveP90: 6}
	want := Estimate(base, p).HumanDays
	for _, cov := range []*float64{nil, new(10.0), new(65.0), new(95.0)} {
		m := base
		m.HasTests, m.TestFuncs, m.CoveragePct = true, 4, cov
		approx(t, "human_days with tests", Estimate(m, p).HumanDays, want, 1e-12)
	}
}

func TestCocomoValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Cocomo)
		wantErr string
	}{
		{name: "profile valid", mutate: func(*Cocomo) {}},
		{name: "basic-81 valid", mutate: func(c *Cocomo) { c.Model = ModelBasic81 }},
		{name: "unknown model", mutate: func(c *Cocomo) { c.Model = "cocomo-iii" }, wantErr: "cocomo.model"},
		{name: "unknown rating", mutate: func(c *Cocomo) { c.Set("prec", "hgh") }, wantErr: `cocomo.prec: unknown rating "hgh"`},
		{name: "rating the driver lacks", mutate: func(c *Cocomo) { c.Set("sced", ratingExtraHigh) },
			wantErr: "cocomo.sced: extra_high is not a rating of sced"},
		{name: "low time", mutate: func(c *Cocomo) { c.Set("time", ratingLow) }, wantErr: "cocomo.time"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := validParams()
			p.Cocomo = rebuildProfile()
			tt.mutate(&p.Cocomo)
			err := p.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}
