package gate

import (
	"cmp"
	"math"
	"slices"
)

// mergePair is one entry of the merge table: on one row, a finding of
// rule drop merges into a finding of rule keep, which then carries both.
// The entry with both metrics empty pairs two findings of the same rule:
// its max_delta breach and its max breach.
type mergePair struct {
	keep, drop string
}

// mergePairs returns the merge table (SPEC.md 8.1), the one place the
// findings that report one cause are declared, in the order merge applies
// them. The same-rule entry comes first, so duplication_pct's max_delta
// and max breaches are one finding before it merges into dup_blocks. The
// kept rule is the one the merged finding is attributed to, the first of
// each pair: sloc, the size tokens_est is estimated from, and dup_blocks,
// whose suggestion already quotes the duplication percentage. A paste
// raises duplicated_lines with dup_blocks, and duplication_pct is judged
// only when duplicated_lines rose, so all three are one finding on
// dup_blocks; when dup_blocks did not fire (a copy that merged two blocks
// into one longer one), duplication_pct merges into duplicated_lines.
// dup_blocks_code, the code blocks of dup_blocks, stands in for dup_blocks
// in a configuration that gates it instead (SPEC.md 8.2): a code paste
// raises it with dup_blocks, so it merges into dup_blocks when both are
// gated, and otherwise carries duplication_pct and duplicated_lines as
// dup_blocks would.
func mergePairs() []mergePair {
	pairs := []mergePair{{}, {keep: "sloc", drop: "tokens_est"}, {keep: "dup_blocks", drop: "dup_blocks_code"}}
	for _, blocks := range []string{"dup_blocks", "dup_blocks_code"} {
		pairs = append(pairs, mergePair{keep: blocks, drop: "duplication_pct"}, mergePair{keep: blocks, drop: "duplicated_lines"})
	}
	return append(pairs, mergePair{keep: "duplicated_lines", drop: "duplication_pct"})
}

// MergedInto returns the metric whose finding a finding of metric is
// merged into when both fire on one row in the same direction (SPEC.md
// 8.1), and false when metric is merged into no other rule's finding. A
// metric the table pairs with two kept rules (duplication_pct) returns
// the first, the one it merges into when all three fire.
func MergedInto(metric string) (string, bool) {
	for _, p := range mergePairs() {
		if !p.sameRule() && p.drop == metric {
			return p.keep, true
		}
	}
	return "", false
}

// sameRule reports whether p is the entry pairing two findings of one
// rule.
func (p mergePair) sameRule() bool { return p.keep == "" && p.drop == "" }

// pairs reports whether b merges into a under p. Only breaches merge, and
// only two reported the same way: both violations or both breaches of
// warn rules, so a merge never changes what fails. A capacity rule's
// ordinary warnings (warn_at band, over the ceiling) never merge, since
// each names its own headroom. Findings of two rules merge only when
// their values moved in the same direction (direction).
func (p mergePair) pairs(a, b *found) bool {
	if a.warning || b.warning || a.v.Severity != b.v.Severity {
		return false
	}
	if p.sameRule() {
		return a.rule == b.rule
	}
	return a.v.Metric == p.keep && b.v.Metric == p.drop && direction(&a.v) == direction(&b.v)
}

// merge folds the findings of fs that report one cause into one, per the
// table of mergePairs, and returns the findings left, in their order. It
// runs after every rule has been evaluated and before the findings are
// split into violations and warnings, so before a warn rule's breach is
// reported as a warning and before exemptions apply. The kept finding's
// Limit names the merged finding's (absorb): "max_delta +6 and max 40"
// for one rule, "max 1000; tokens_est 15000 -> 17500, max 16000" for two,
// and its Also lists the merged rules. fs is modified.
func merge(fs []found) []found {
	for _, p := range mergePairs() {
		for i := 0; i < len(fs); i++ {
			for j := 0; j < len(fs); j++ {
				if j == i || (p.sameRule() && j < i) || !p.pairs(&fs[i], &fs[j]) {
					continue
				}
				absorb(&fs[i].v, &fs[j].v, !p.sameRule())
				fs = slices.Delete(fs, j, j+1)
				if j < i {
					i--
				}
				j--
			}
		}
	}
	return fs
}

// absorb merges d into k. For the same rule, k's limit gains " and " and
// d's ("max_delta +6 and max 40") and k takes d's Function when it has
// none; for another rule (other), it gains "; " and d's metric, values and
// limit, and k's Also gains d's metric and d's own Also.
func absorb(k, d *Violation, other bool) {
	if !other {
		k.Limit += " and " + d.Limit
		if k.Function == nil {
			k.Function = d.Function
		}
		return
	}
	k.Limit += "; " + d.Metric + " " + values(d) + ", " + d.Limit
	for _, m := range append([]string{d.Metric}, d.Also...) {
		if m != k.Metric && !slices.Contains(k.Also, m) {
			k.Also = append(k.Also, m)
		}
	}
}

// values renders v's values as the text format does, "<base> -> <head>",
// or "<head>" without a baseline value, to at most two decimals.
func values(v *Violation) string {
	h := num(round2(v.Head))
	if !v.HasBase {
		return h
	}
	return num(round2(v.Base)) + " -> " + h
}

// round2 rounds v to two decimals.
func round2(v float64) float64 { return math.Round(v*100) / 100 }

// direction is the sign of v's change from its baseline value: 1 for a
// rise or for a value with no baseline, 0 for none and -1 for a fall.
func direction(v *Violation) int {
	if !v.HasBase {
		return 1
	}
	return cmp.Compare(v.Head, v.Base)
}
