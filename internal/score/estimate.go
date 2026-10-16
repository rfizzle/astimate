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
	// HumanDays is the human estimate in working days under the configured
	// COCOMO model (SPEC.md 7.3).
	HumanDays float64
	// HumanDaysLow and HumanDaysHigh are the lower and the higher of the
	// two models' estimates, COCOMO 81 and COCOMO II under the rebuild
	// profile, on the same kloc_eff: the range the reports print, since
	// no single figure is measured.
	HumanDaysLow, HumanDaysHigh float64
	// HumanLowModel and HumanHighModel name the model of each end, as
	// ModelName does.
	HumanLowModel, HumanHighModel string
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
// 7.3. It reads the v0 fields and one v1 field, coverage_pct, which scales
// the unspecified term when non-null (unspecifiedTerm) and rates RELY for
// the human estimate; no other v1 pointer field changes the result. fan_in is recorded in the contract term's
// Detail but does not enter the formula. cfg is assumed to have passed
// Validate.
func Estimate(m metrics.RawMetrics, cfg RebuildParams) Rebuild {
	keep := 1 - m.DuplicationPct/100
	volume := float64(m.TokensEst) * keep
	spec := math.Max(float64(m.TokensEstWithTests-m.TokensEst), 0)
	contract := float64(m.ExportedSymbols) * cfg.TokensPerExport
	unspecified, unspecifiedDetail := unspecifiedTerm(&m, cfg)
	hidden := float64(m.Globals+m.InitFuncs) * cfg.TokensPerHiddenState

	terms := []Term{
		{Name: TermVolume, Tokens: volume, Detail: "tokens_est=" + strconv.Itoa(m.TokensEst) +
			" duplication_pct=" + strconv.FormatFloat(m.DuplicationPct, 'g', -1, 64)},
		{Name: TermSpec, Tokens: spec, Detail: "tokens_est_with_tests=" + strconv.Itoa(m.TokensEstWithTests) +
			" tokens_est=" + strconv.Itoa(m.TokensEst) + " test_funcs=" + strconv.Itoa(m.TestFuncs)},
		{Name: TermContract, Tokens: contract, Detail: "exported_symbols=" + strconv.Itoa(m.ExportedSymbols) +
			" fan_in=" + strconv.Itoa(m.FanIn)},
		{Name: TermUnspecified, Tokens: unspecified, Detail: unspecifiedDetail},
		{Name: TermHidden, Tokens: hidden, Detail: "globals=" + strconv.Itoa(m.Globals) +
			" init_funcs=" + strconv.Itoa(m.InitFuncs)},
	}

	total := volume + spec + contract + unspecified + hidden
	ratio := total / cfg.ContextBudget
	passes := ratio
	if ratio > 1 {
		passes = math.Pow(ratio, cfg.SuperlinearExponent)
	}

	r := Rebuild{RebuildTokens: total, Ratio: ratio, AgentPasses: passes, Terms: terms}
	r.setHuman(&m, cfg, keep)
	return r
}

// unspecifiedTerm returns the unspecified-behavior term of SPEC.md 7.2 and
// its Detail. Without coverage_pct it is untested_exports *
// tokens_per_untested_export, a step penalty per untested export. With it,
// that product is scaled by 1 - coverage_pct/100, so a package whose tests
// execute its statements, if not by naming every export, owes less
// reverse-engineering; the Detail then records coverage_pct too.
func unspecifiedTerm(m *metrics.RawMetrics, cfg RebuildParams) (tokens float64, detail string) {
	tokens = float64(m.UntestedExports) * cfg.TokensPerUntestedExport
	detail = "untested_exports=" + strconv.Itoa(m.UntestedExports)
	if m.CoveragePct == nil {
		return tokens, detail
	}
	pct := min(max(*m.CoveragePct, 0), 100)
	return tokens * (1 - pct/100), detail + " coverage_pct=" + strconv.FormatFloat(pct, 'g', -1, 64)
}

// setHuman sets the human estimate of SPEC.md 7.3: HumanDays under the
// configured model, COCOMO II with the rebuild profile or COCOMO 81 basic
// organic mode, and the range from the lower of the two to the higher.
func (r *Rebuild) setHuman(m *metrics.RawMetrics, cfg RebuildParams, keep float64) {
	b81, ii := HumanDaysBasic81(m, cfg, keep), HumanDaysCOCOMOII(m, cfg, keep)
	r.HumanDays = ii
	if cfg.Cocomo.Model == ModelBasic81 {
		r.HumanDays = b81
	}
	r.HumanDaysLow, r.HumanLowModel = b81, ModelName(ModelBasic81)
	r.HumanDaysHigh, r.HumanHighModel = ii, ModelName(ModelCOCOMOII)
	if ii < b81 {
		r.HumanDaysLow, r.HumanDaysHigh = r.HumanDaysHigh, r.HumanDaysLow
		r.HumanLowModel, r.HumanHighModel = r.HumanHighModel, r.HumanLowModel
	}
}
