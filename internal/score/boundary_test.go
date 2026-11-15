package score

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// TestImportBoundary checks that score depends on metrics and its leaf
// role only, and that its sub-package suggestion, which words the fix
// suggestions, adds score itself and nothing else from this module, so
// the score tree stays below the extractors, the gate and the reports.
func TestImportBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	t.Parallel()

	for pkg, allowed := range map[string][]string{
		".":            {"internal/metrics", "internal/metrics/role"},
		"./suggestion": {"internal/metrics", "internal/score"},
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
