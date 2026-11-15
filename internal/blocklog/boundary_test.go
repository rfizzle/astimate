package blocklog

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// TestImportBoundary checks that blocklog reads a check's rows and the
// configuration only: it records decisions, so it never reaches the
// engine, an extractor or the baseline, and cmd/astimate may import it
// without going around internal/engine.
func TestImportBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	t.Parallel()

	allowed := []string{"internal/config", "internal/gate", "internal/gate/exempt", "internal/report"}
	out, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Imports "\n"}}`, ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for imp := range strings.FieldsSeq(string(out)) {
		_, rel, ok := strings.Cut(imp, "/astimate/")
		if ok && !slices.Contains(allowed, rel) {
			t.Errorf("blocklog imports %s; it may import only %q from this module", imp, allowed)
		}
	}
}
