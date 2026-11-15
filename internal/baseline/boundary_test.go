package baseline

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// TestImportBoundary checks that the packages under baseline's internal
// directory import only what AGENTS.md allows them from this module: the
// file layout (fileformat) reads and writes bytes and imports metrics and
// its leaf inventory alone, never git, an extractor or baseline itself.
func TestImportBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	t.Parallel()

	for pkg, allowed := range map[string][]string{
		"./internal/fileformat": {"internal/metrics", "internal/metrics/inventory"},
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
			if imp == "os/exec" {
				t.Errorf("%s imports os/exec; only baseline calls git", pkg)
			}
		}
	}
}
