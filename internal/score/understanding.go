package score

import (
	"slices"

	"github.com/rfizzle/astimate/internal/metrics"
)

// UnfmKeys names the unfamiliarity ratings rebuild.cocomo.unfm takes, from
// the most familiar to the least, in the order of unfmTable (SPEC.md 7.3).
func UnfmKeys() []string {
	return []string{"completely_familiar", "mostly_familiar", "somewhat_familiar",
		"considerably_familiar", "mostly_unfamiliar", "completely_unfamiliar"}
}

// unfmTable holds COCOMO II.2000's reuse-model UNFM values in UnfmKeys
// order.
func unfmTable() [6]float64 {
	return [6]float64{0, 0.2, 0.4, 0.6, 0.8, 1}
}

// unfm returns the value of the unfamiliarity rating name, and false for a
// name UnfmKeys lacks, the empty name among them: the configuration always
// names one.
func unfm(name string) (float64, bool) {
	i := slices.Index(UnfmKeys(), name)
	if i < 0 {
		return 0, false
	}
	return unfmTable()[i], true
}

// The SU sub-scores with cut points in suCuts.
const (
	cutCognitive = iota
	cutDup
	cutMissing
)

// suCuts returns the cut points of the SU sub-score metric, one of
// cutCognitive (cognitive_p90), cutDup (dup_blocks) and cutMissing (the
// share of exports untested or undocumented), SPEC.md 7.3: each the largest
// value still rated very_high, high, nominal and low; above the last is
// very_low.
func suCuts(metric int) [4]float64 {
	return [3][4]float64{
		{4, 10, 20, 40},
		{0, 2, 5, 10},
		{0.05, 0.15, 0.35, 0.6},
	}[metric]
}

// band rates v against cuts: 4 (very_high) at or below cuts[0], down to 0
// (very_low) above cuts[3].
func band(v float64, cuts [4]float64) int {
	for i, c := range cuts {
		if v <= c {
			return 4 - i
		}
	}
	return 0
}

// understanding is SPEC.md 7.3's SU for m, in percent: the mean of the
// structure, application-clarity and self-descriptiveness sub-scores, each
// 0 (very_low) to 4 (very_high), rounded down, toward the worse rating,
// picks COCOMO II.2000's SU, very_low 50 to very_high 10.
func understanding(m *metrics.RawMetrics) float64 {
	return [5]float64{50, 40, 30, 20, 10}[(structure(m)+clarity(m)+selfDescription(m))/3]
}

// structure rates cognitive_p90 and dup_blocks, a copy being read twice,
// and takes the worse of the two.
func structure(m *metrics.RawMetrics) int {
	return min(band(float64(m.CognitiveP90), suCuts(cutCognitive)), band(float64(m.DupBlocks), suCuts(cutDup)))
}

// clarity rates the share of exported symbols that are untested exports,
// the behaviour no test describes. Testing more exports never lowers it.
func clarity(m *metrics.RawMetrics) int {
	return band(missingShare(m.UntestedExports, m.ExportedSymbols), suCuts(cutMissing))
}

// selfDescription rates the share of exported symbols without a doc
// comment; nominal when documented_exports is not measured.
func selfDescription(m *metrics.RawMetrics) int {
	if m.DocumentedExports == nil {
		return 2
	}
	return band(missingShare(m.ExportedSymbols-*m.DocumentedExports, m.ExportedSymbols), suCuts(cutMissing))
}

// missingShare is n over exports, clamped to [0, 1]; 0 without exports.
func missingShare(n, exports int) float64 {
	return min(max(float64(n)/float64(max(exports, 1)), 0), 1)
}
