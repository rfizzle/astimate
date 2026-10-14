package judge

import (
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// TestCrossJudgmentRecorded checks what a check hands the gate replay: the
// groups with module-relative packages when the added blocks were judged,
// an empty list when no set gained one, and nil when the full count was.
func TestCrossJudgmentRecorded(t *testing.T) {
	t.Parallel()

	n := 3
	m := metrics.RawMetrics{DupBlocksCrossPkg: &n}
	bm := &metrics.RawMetrics{DupBlocksCrossPkg: &n}
	block := metrics.CrossBlock{Tokens: 90, Occurrences: []metrics.Occurrence{{Package: "ex.com/m/a"}, {Package: "ex.com/m/b"}}}
	rel := func(pkg string) string { return strings.TrimPrefix(pkg, "ex.com/m/") }
	groups := []score.CrossGroup{{Base: []int{70}, Head: []metrics.CrossBlock{block}}}

	got := crossJudgment{known: true, groups: groups}.recorded(m, bm, rel)
	if len(got) != 1 || got[0].Head[0].Occurrences[1].Package != "b" || got[0].Base[0] != 70 {
		t.Errorf("recorded = %+v, want one group with packages a and b", got)
	}
	if groups[0].Head[0].Occurrences[1].Package != "ex.com/m/b" {
		t.Error("recorded renamed the judgment's own blocks")
	}
	if got := (crossJudgment{known: true}).recorded(m, bm, rel); got == nil || len(got) != 0 {
		t.Errorf("no group gained a block: recorded = %#v, want an empty list", got)
	}
	if got := (crossJudgment{known: false, groups: groups}).recorded(m, bm, rel); got != nil {
		t.Errorf("full count judged: recorded = %+v, want nil", got)
	}
	if got := (crossJudgment{known: true, groups: groups}).recorded(m, nil, rel); got != nil {
		t.Errorf("no baseline module row: recorded = %+v, want nil", got)
	}
}
