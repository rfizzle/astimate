package narrate

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// TestImportBoundary checks that narrate and compose import only the
// report packages and metrics from this module, never an extractor,
// score, gate or baseline.
func TestImportBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	t.Parallel()

	for pkg, allowed := range map[string][]string{
		".":         {"internal/report"},
		"./compose": {"internal/report", "internal/report/rows", "internal/report/details", "internal/metrics", "internal/narrate"},
	} {
		out, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Imports "\n"}}`, pkg).Output()
		if err != nil {
			t.Fatalf("go list %s: %v", pkg, err)
		}
		for imp := range strings.FieldsSeq(string(out)) {
			_, rel, ok := strings.Cut(imp, "/astimate/")
			if ok && !slices.Contains(allowed, rel) {
				t.Errorf("%s imports %s; it may import only %q from this module", pkg, imp, allowed)
			}
		}
	}
}
