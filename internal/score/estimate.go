package score

import (
	"math"
	"strconv"

	"github.com/rfizzle/astimate/internal/metrics"
)

// Term names, in the fixed order Rebuild.Terms uses.
const (
	TermVolume      = "volume"
	TermSpec        = "spec"
	TermContract    = "contract"
	TermUnspecified = "unspecified"
	TermHidden      = "hidden"
)

// Term is one additive component of rebuild_tokens (SPEC.md 7.2).
type Term struct {
	// Name is one of volume, spec, contract, unspecified or hidden.
	Name string
	// Tokens is the term's contribution to rebuild_tokens.
	Tokens float64
	// Detail lists the metric values the term was computed from, as
	// space-separated name=value pairs.
	Detail string
}

// Rebuild is the rebuild estimate for one package (SPEC.md 7.2 and 7.3).
// Values keep full precision; round only for display.
type Rebuild struct {
	// RebuildTokens is the sum of the terms' tokens.
	RebuildTokens float64
	// Ratio is RebuildTokens divided by the context budget.
	Ratio float64
	// AgentPasses is Ratio up to 1 and Ratio raised to the superlinear
	// exponent beyond it.
	AgentPasses float64
	// HumanDays is the COCOMO basic organic-mode estimate in working days.
	HumanDays float64
	// Terms holds the five rebuild_tokens terms in the fixed order volume,
	// spec, contract, unspecified, hidden.
	Terms []Term
}

// AgentPassesRounded returns AgentPasses rounded to one decimal, the
// precision SPEC.md 7.2 reports.
func (r Rebuild) AgentPassesRounded() float64 {
	return math.Round(r.AgentPasses*10) / 10
}

// Estimate computes the rebuild estimate for m under cfg, per SPEC.md 7.1 to
// 7.3. It reads only v0 fields: v1 pointer fields, including coverage_pct,
// never change the result. fan_in is recorded in the contract term's Detail
// but does not enter the formula. cfg is assumed to have passed Validate.
func Estimate(m metrics.RawMetrics, cfg RebuildParams) Rebuild {
	keep := 1 - m.DuplicationPct/100
	volume := float64(m.TokensEst) * keep
	spec := math.Max(float64(m.TokensEstWithTests-m.TokensEst), 0)
	contract := float64(m.ExportedSymbols) * cfg.TokensPerExport
	unspecified := float64(m.UntestedExports) * cfg.TokensPerUntestedExport
	hidden := float64(m.Globals+m.InitFuncs) * cfg.TokensPerHiddenState

	terms := []Term{
		{Name: TermVolume, Tokens: volume, Detail: "tokens_est=" + strconv.Itoa(m.TokensEst) +
			" duplication_pct=" + strconv.FormatFloat(m.DuplicationPct, 'g', -1, 64)},
		{Name: TermSpec, Tokens: spec, Detail: "tokens_est_with_tests=" + strconv.Itoa(m.TokensEstWithTests) +
			" tokens_est=" + strconv.Itoa(m.TokensEst) + " test_funcs=" + strconv.Itoa(m.TestFuncs)},
		{Name: TermContract, Tokens: contract, Detail: "exported_symbols=" + strconv.Itoa(m.ExportedSymbols) +
			" fan_in=" + strconv.Itoa(m.FanIn)},
		{Name: TermUnspecified, Tokens: unspecified, Detail: "untested_exports=" + strconv.Itoa(m.UntestedExports)},
		{Name: TermHidden, Tokens: hidden, Detail: "globals=" + strconv.Itoa(m.Globals) +
			" init_funcs=" + strconv.Itoa(m.InitFuncs)},
	}

	total := volume + spec + contract + unspecified + hidden
	ratio := total / cfg.ContextBudget
	passes := ratio
	if ratio > 1 {
		passes = math.Pow(ratio, cfg.SuperlinearExponent)
	}

	return Rebuild{
		RebuildTokens: total,
		Ratio:         ratio,
		AgentPasses:   passes,
		HumanDays:     humanDays(&m, cfg, keep),
		Terms:         terms,
	}
}

// humanDays is SPEC.md 7.3: COCOMO basic organic mode on the non-duplicated
// KLOC, inflated by up to half for untested exports. keep is the
// non-duplicated share, 1 - duplication_pct/100.
func humanDays(m *metrics.RawMetrics, cfg RebuildParams, keep float64) float64 {
	untestedRatio := float64(m.UntestedExports) / float64(max(m.ExportedSymbols, 1))
	untestedRatio = min(max(untestedRatio, 0), 1)
	klocEff := (float64(m.SLOC) * keep / 1000) * (1 + 0.5*untestedRatio)
	personMonths := cfg.CocomoA * math.Pow(klocEff, cfg.CocomoB)
	return personMonths * cfg.DaysPerMonth
}
