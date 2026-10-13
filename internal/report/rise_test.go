package report

import (
	"strings"
	"testing"
)

// TestRiseCondition checks the re-export the MCP server explains the
// gate's direction test with: duplication_pct and cognitive_p90 have one,
// every other metric none.
func TestRiseCondition(t *testing.T) {
	t.Parallel()

	for metric, want := range map[string]string{
		"duplication_pct": "duplicated_lines",
		"cognitive_p90":   "baseline's p90",
		"max_nesting":     "",
	} {
		got, ok := RiseCondition(metric)
		if ok != (want != "") || !strings.Contains(got, want) {
			t.Errorf("RiseCondition(%q) = %q, %v; want one containing %q", metric, got, ok, want)
		}
	}
}

// TestMergedInto checks the re-export the MCP server explains merges with.
func TestMergedInto(t *testing.T) {
	t.Parallel()

	if into, ok := MergedInto("tokens_est"); !ok || into != "sloc" {
		t.Errorf("MergedInto(tokens_est) = %q, %v; want sloc", into, ok)
	}
	if _, ok := MergedInto("sloc"); ok {
		t.Error("MergedInto(sloc) reports a merge, want none")
	}
}
