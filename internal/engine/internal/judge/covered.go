package judge

import (
	"slices"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// coveredBase returns the baseline metrics a source: coverage rule judges
// m against (SPEC.md 8.1): bm itself, unless head has
// untested_exports_covered and the baseline, which never runs tests, does
// not. Then it is a copy of bm whose untested_exports_covered is the
// baseline's untested exports less those head's coverage run executed
// (covered, metrics.Details.UntestedCovered), on the reading that an
// export the change kept runs at the baseline as it does at head, so the
// rule judges what the change added rather than what the opt-in settled.
// Without the baseline's names (baseline.Row.HasNamed false, as in a
// baseline file written before they were recorded) bm is returned
// unchanged and the rule falls back to the static counts on both sides.
func coveredBase(m metrics.RawMetrics, bm *metrics.RawMetrics, row *baseline.Row, covered []string) *metrics.RawMetrics {
	if bm == nil || m.UntestedExportsCovered == nil || bm.UntestedExportsCovered != nil || !row.HasNamed {
		return bm
	}
	n := 0
	for _, name := range row.Named.UntestedExports {
		if !slices.Contains(covered, name) {
			n++
		}
	}
	gb := *bm
	gb.UntestedExportsCovered = &n
	return &gb
}

// coveredLast moves the untested exports of d that a coverage run executed
// (d.UntestedCovered) after the others, each part in the order rankNew
// gave it, with their positions, and names' untested exports to match
// when rankNew set them, so a finding cites and is located on an export
// no test ran. l.untested then holds only when the first export left is
// one the change added: the package is new at head (inBase false) or base,
// the baseline's untested exports, lacks it. Without a run d is unchanged.
func coveredLast(d *metrics.Details, names *score.Names, l *leads, base []string, inBase bool) {
	if len(d.UntestedCovered) == 0 {
		return
	}
	ran := make([]bool, len(d.UntestedExports))
	var first string
	for i, name := range d.UntestedExports {
		ran[i] = slices.Contains(d.UntestedCovered, name)
		if !ran[i] && first == "" {
			first = name
		}
	}
	d.UntestedExports = notRanFirst(d.UntestedExports, ran)
	if len(d.UntestedPositions) == len(ran) {
		d.UntestedPositions = notRanFirst(d.UntestedPositions, ran)
	}
	if names.UntestedExports != nil {
		names.UntestedExports = d.UntestedExports
	}
	l.untested = l.untested && first != "" && (!inBase || !slices.Contains(base, first))
}

// notRanFirst returns s with the items whose ran is false first, each part
// in the order of s.
func notRanFirst[T any](s []T, ran []bool) []T {
	out := make([]T, 0, len(s))
	for i := range s {
		if !ran[i] {
			out = append(out, s[i])
		}
	}
	for i := range s {
		if ran[i] {
			out = append(out, s[i])
		}
	}
	return out
}
