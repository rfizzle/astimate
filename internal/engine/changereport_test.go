package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/report"
)

// TestAppendSummary appends to a step summary that already has content and
// says how many findings the cap left without an annotation.
func TestAppendSummary(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "summary.md")
	if err := os.WriteFile(path, []byte("earlier step\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &narrate.Module{Change: &narrate.Change{Verdict: "FAIL", Changed: 25, Failed: 25, Violations: 25}}
	note := report.OmittedText(report.Omitted{Findings: 15})
	if err := appendSummary(path, m, note); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"earlier step\n## astimate: FAIL — 25 of 25 changed packages need attention\n",
		"\n15 more findings are not annotated; the report lists them all.\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary lacks %q:\n%s", want, got)
		}
	}
}
