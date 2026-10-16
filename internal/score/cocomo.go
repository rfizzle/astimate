package score

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
)

// The models rebuild.cocomo.model names (SPEC.md 7.3).
const (
	// ModelCOCOMOII is COCOMO II.2000's post-architecture form under the
	// rated profile, the default.
	ModelCOCOMOII = "cocomo-ii"
	// ModelBasic81 is COCOMO 81 basic organic mode, cocomo_a and cocomo_b.
	ModelBasic81 = "basic-81"
)

// Rating is a COCOMO II driver rating, named as the configuration names it.
// The zero Rating is nominal.
type Rating string

// The six ratings, in the order of the published tables.
const (
	ratingVeryLow   Rating = "very_low"
	ratingLow       Rating = "low"
	ratingNominal   Rating = "nominal"
	ratingHigh      Rating = "high"
	ratingVeryHigh  Rating = "very_high"
	ratingExtraHigh Rating = "extra_high"
)

// COCOMO II.2000 post-architecture calibration (Boehm et al., Software Cost
// Estimation with COCOMO II, 2000).
const (
	cocomoIIA = 2.94
	cocomoIIB = 0.91
	// scaleFactors is how many of the drivers CocomoKeys names, first,
	// are scale factors; the rest are effort multipliers.
	scaleFactors = 5
	// configured is how many drivers the configuration rates.
	configured = 20
	// cplx is the row of CPLX, the per-package multiplier, in
	// cocomoTable, after the configured drivers.
	cplx = configured
)

// CocomoKeys names the drivers rebuild.cocomo rates, in the order of
// Cocomo.Ratings: the five scale factors, then the effort multipliers the
// code cannot know. CPLX is rated from each package instead, and RELY is
// nominal: the tests are the specification, not a reliability requirement.
func CocomoKeys() []string {
	return []string{"prec", "flex", "resl", "team", "pmat",
		"data", "ruse", "docu", "time", "stor", "pvol", "acap", "pcap", "pcon", "apex", "plex", "ltex", "tool", "site", "sced"}
}

// cocomoTable holds the published values of the drivers in CocomoKeys
// order, then CPLX, very_low to extra_high; -1 marks a rating the driver
// does not have.
func cocomoTable() [configured + 1][6]float64 {
	return [configured + 1][6]float64{
		{6.20, 4.96, 3.72, 2.48, 1.24, 0}, // PREC
		{5.07, 4.05, 3.04, 2.03, 1.01, 0}, // FLEX
		{7.07, 5.65, 4.24, 2.83, 1.41, 0}, // RESL
		{5.48, 4.38, 3.29, 2.19, 1.10, 0}, // TEAM
		{7.80, 6.24, 4.68, 3.12, 1.56, 0}, // PMAT
		{-1, 0.90, 1, 1.14, 1.28, -1},     // DATA
		{-1, 0.95, 1, 1.07, 1.15, 1.24},   // RUSE
		{0.81, 0.91, 1, 1.11, 1.23, -1},   // DOCU
		{-1, -1, 1, 1.11, 1.29, 1.63},     // TIME
		{-1, -1, 1, 1.05, 1.17, 1.46},     // STOR
		{-1, 0.87, 1, 1.15, 1.30, -1},     // PVOL
		{1.42, 1.19, 1, 0.85, 0.71, -1},   // ACAP
		{1.34, 1.15, 1, 0.88, 0.76, -1},   // PCAP
		{1.29, 1.12, 1, 0.90, 0.81, -1},   // PCON
		{1.22, 1.10, 1, 0.88, 0.81, -1},   // APEX
		{1.19, 1.09, 1, 0.91, 0.85, -1},   // PLEX
		{1.20, 1.09, 1, 0.91, 0.84, -1},   // LTEX
		{1.17, 1.09, 1, 0.90, 0.78, -1},   // TOOL
		{1.22, 1.09, 1, 0.93, 0.86, 0.80}, // SITE
		{1.43, 1.14, 1, 1, 1, -1},         // SCED
		{0.73, 0.87, 1, 1.17, 1.34, 1.74}, // CPLX
	}
}

// Cocomo is rebuild.cocomo: the model and the profile's ratings.
type Cocomo struct {
	// Model is ModelCOCOMOII or ModelBasic81; empty is ModelCOCOMOII.
	Model string
	// Ratings rates the drivers CocomoKeys names, in its order.
	Ratings [configured]Rating
	// UNFM is the reuse model's unfamiliarity with the code to be
	// understood, one of UnfmKeys; empty is completely_unfamiliar.
	UNFM string
}

// Set rates the driver key, one of CocomoKeys, r. It reports false, and
// changes nothing, for any other key.
func (c *Cocomo) Set(key string, r Rating) bool {
	i := slices.Index(CocomoKeys(), key)
	if i < 0 {
		return false
	}
	c.Ratings[i] = r
	return true
}

// index returns r's column in the tables, -1 for an unknown name.
func (r Rating) index() int {
	switch r {
	case ratingVeryLow:
		return 0
	case ratingLow:
		return 1
	case ratingNominal, "":
		return 2
	case ratingHigh:
		return 3
	case ratingVeryHigh:
		return 4
	case ratingExtraHigh:
		return 5
	}
	return -1
}

// value returns the published value of the driver in row of cocomoTable
// under r, the nominal one when the driver has no such rating (Validate
// reports it).
func value(row int, r Rating) float64 {
	t := cocomoTable()[row]
	if i := r.index(); i >= 0 && t[i] >= 0 {
		return t[i]
	}
	return t[2]
}

// validate reports a model other than the two, and every rating that is not
// a rating name or that the driver does not have.
func (c Cocomo) validate() error {
	var errs []error
	if c.Model != "" && c.Model != ModelCOCOMOII && c.Model != ModelBasic81 {
		errs = append(errs, fmt.Errorf("cocomo.model must be %s or %s, got %q", ModelCOCOMOII, ModelBasic81, c.Model))
	}
	table, keys := cocomoTable(), CocomoKeys()
	for i, r := range c.Ratings {
		switch j := r.index(); {
		case j < 0:
			errs = append(errs, fmt.Errorf("cocomo.%s: unknown rating %q, want very_low, low, nominal, high, very_high or extra_high",
				keys[i], r))
		case table[i][j] < 0:
			errs = append(errs, fmt.Errorf("cocomo.%s: %s is not a rating of %s", keys[i], r, keys[i]))
		}
	}
	if _, ok := unfm(c.UNFM); !ok {
		errs = append(errs, fmt.Errorf("cocomo.unfm: unknown rating %q, want one of %s", c.UNFM, strings.Join(UnfmKeys(), ", ")))
	}
	return errors.Join(errs...)
}

// Exponent is the profile's E = B + 0.01 × ΣSF.
func (c Cocomo) Exponent() float64 {
	sum := 0.0
	for i, r := range c.Ratings[:scaleFactors] {
		sum += value(i, r)
	}
	return cocomoIIB + 0.01*sum
}

// Coefficient is A times the product of the configured effort multipliers:
// person-months per KSLOC before the exponent and CPLX.
func (c Cocomo) Coefficient() float64 {
	a := cocomoIIA
	for i := scaleFactors; i < configured; i++ {
		a *= value(i, c.Ratings[i])
	}
	return a
}

// Name is the model as the reports name it.
func (c Cocomo) Name() string {
	if c.Model == ModelBasic81 {
		return "COCOMO 81"
	}
	return "COCOMO II, rebuild profile"
}

// effectiveKLOC is SPEC.md 7.3's kloc_eff: the non-duplicated KLOC,
// inflated by COCOMO II's reuse-model understanding increment,
// 1 + 0.02 × SU × UNFM, with SU rated from m (understanding) and UNFM the
// profile's c.UNFM. Both models share it. keep is the non-duplicated share,
// 1 - duplication_pct/100.
func effectiveKLOC(m *metrics.RawMetrics, c *Cocomo, keep float64) float64 {
	u, _ := unfm(c.UNFM)
	return (float64(m.SLOC) * keep / 1000) * (1 + 0.02*understanding(m)*u)
}

// HumanDaysBasic81 is the human estimate under COCOMO 81 basic organic mode,
// cocomo_a × kloc_eff^cocomo_b person-months. keep is the non-duplicated
// share, 1 - duplication_pct/100.
func HumanDaysBasic81(m *metrics.RawMetrics, cfg RebuildParams, keep float64) float64 {
	return cfg.CocomoA * math.Pow(effectiveKLOC(m, &cfg.Cocomo, keep), cfg.CocomoB) * cfg.DaysPerMonth
}

// HumanDaysCOCOMOII is the human estimate under COCOMO II's
// post-architecture form with cfg.Cocomo's profile, Coefficient ×
// kloc_eff^Exponent × CPLX person-months, CPLX rated from m (cplxRating)
// and RELY nominal. keep is as for HumanDaysBasic81.
func HumanDaysCOCOMOII(m *metrics.RawMetrics, cfg RebuildParams, keep float64) float64 {
	c := &cfg.Cocomo
	pm := c.Coefficient() * math.Pow(effectiveKLOC(m, c, keep), c.Exponent()) * value(cplx, cplxRating(m))
	return pm * cfg.DaysPerMonth
}

// cplxRating rates product complexity from cognitive_p90: up to 1 very
// low, 4 low, 10 nominal, 20 high, 40 very high, extra high above; nominal
// for a package without functions, where it is not measured.
func cplxRating(m *metrics.RawMetrics) Rating {
	p := m.CognitiveP90
	switch {
	case m.FuncCount == 0:
		return ratingNominal
	case p <= 1:
		return ratingVeryLow
	case p <= 4:
		return ratingLow
	case p <= 10:
		return ratingNominal
	case p <= 20:
		return ratingHigh
	case p <= 40:
		return ratingVeryHigh
	}
	return ratingExtraHigh
}
