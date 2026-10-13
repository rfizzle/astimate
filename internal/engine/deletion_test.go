package engine

import (
	"strconv"
	"testing"
)

// show renders an optional count for a test message.
func show(p *int) string {
	if p == nil {
		return "null"
	}
	return strconv.Itoa(*p)
}

// hardSource returns one function whose cognitive complexity is well over
// the cognitive_p90 floor of 10.
func hardSource() string {
	return "package p\n\n// Hard classifies xs.\nfunc Hard(xs []int, m map[int]bool) int {\n\tn := 0\n" +
		"\tfor _, x := range xs {\n\t\tif x > 0 {\n\t\t\tif m[x] {\n\t\t\t\tn++\n\t\t\t} else if x%2 == 0 {\n" +
		"\t\t\t\tn += 2\n\t\t\t} else {\n\t\t\t\tswitch {\n\t\t\t\tcase x > 10 && x < 20:\n\t\t\t\t\tn--\n" +
		"\t\t\t\tcase x > 100 || x < -100:\n\t\t\t\t\tn -= 2\n\t\t\t\t}\n\t\t\t}\n\t\t}\n\t}\n\treturn n\n}\n"
}

// trivialSource returns seven trivial functions of distinct shapes, so
// they hold no duplicate block, that a deletion-only change removes.
func trivialSource() string {
	return "package p\n\n// A is one.\nfunc A() int {\n\treturn 1\n}\n\n// B is positive.\nfunc B(x int) bool {\n\treturn x > 0\n}\n" +
		"\n// C shouts.\nfunc C(s string) string {\n\treturn s + \"!\"\n}\n\n// D counts.\nfunc D(xs []int) int {\n\treturn len(xs)\n}\n" +
		"\n// E deletes.\nfunc E(m map[string]int, k string) {\n\tdelete(m, k)\n}\n\n// F zeroes.\nfunc F(p *int) {\n\t*p = 0\n}\n" +
		"\n// G multiplies.\nfunc G(a, b float64) float64 {\n\treturn a * b\n}\n"
}

// TestCheckDeletionOnly checks a change that only deletes code: seven
// trivial functions go, so the package's duplication share rises past the
// duplication_pct max under the same duplicate block and its cognitive
// p90 rises from a trivial function's to the most complex one's. Neither
// rule fails, since the change raised neither the duplicated lines nor any
// function's complexity (SPEC.md 8.1).
func TestCheckDeletionOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	dir := gitRepoAt(t, map[string]string{
		"go.mod":       "module example.com/m\n\ngo 1.22\n",
		"p/a.go":       foldSource(""),
		"p/hard.go":    hardSource(),
		"p/trivial.go": trivialSource(),
	})
	gitIn(t, dir, "rm", "-q", "p/trivial.go")
	gitIn(t, dir, "commit", "-q", "--no-verify", "-m", "delete")

	c, logs := checkAgainstBase(t, dir, false)
	p := findRow(c, "p")
	if p == nil || p.Baseline == nil {
		t.Fatalf("checked %d packages, want p with a baseline; logs:\n%s", len(c.Packages), logs)
	}
	head, base := p.Metrics, p.Baseline.Metrics
	if head.DuplicatedLines == nil || base.DuplicatedLines == nil || *head.DuplicatedLines != *base.DuplicatedLines ||
		*head.DuplicatedLines == 0 {
		t.Fatalf("duplicated_lines %s -> %s, want the same positive count", show(base.DuplicatedLines), show(head.DuplicatedLines))
	}
	if head.DuplicationPct <= 40 || base.DuplicationPct >= 40 {
		t.Errorf("duplication_pct %v -> %v, want a crossing of the max 40", base.DuplicationPct, head.DuplicationPct)
	}
	if head.CognitiveP90 <= 10 || head.CognitiveP90-base.CognitiveP90 <= 3 {
		t.Errorf("cognitive_p90 %d -> %d, want a rise of more than 3 past the floor 10", base.CognitiveP90, head.CognitiveP90)
	}
	if head.ChangedFuncCognitiveMax == nil || *head.ChangedFuncCognitiveMax != 0 {
		t.Errorf("changed_func_cognitive_max = %s, want 0: no function changed", show(head.ChangedFuncCognitiveMax))
	}
	for _, f := range p.Violations {
		if f.Metric == "duplication_pct" || f.Metric == "cognitive_p90" {
			t.Errorf("violation %+v, want none on a deletion-only change", f)
		}
	}
}
