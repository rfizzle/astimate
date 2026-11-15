package mcpserver

import (
	"os/exec"
	"strings"
	"testing"
)

// TestImportBoundary checks that the server and the packages under its
// internal directory reach the extractors, baseline, gate and score only
// through internal/engine, never by importing them directly.
func TestImportBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	t.Parallel()

	out, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{.ImportPath}}{{range .Imports}} {{.}}{{end}}`, "./...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	listed := 0
	for line := range strings.Lines(string(out)) {
		pkg, imports, _ := strings.Cut(strings.TrimSpace(line), " ")
		listed++
		for imp := range strings.FieldsSeq(imports) {
			_, rel, ok := strings.Cut(imp, "/astimate/")
			if !ok {
				continue
			}
			for _, forbidden := range []string{"internal/lang", "internal/baseline", "internal/gate", "internal/score"} {
				if rel == forbidden || strings.HasPrefix(rel, forbidden+"/") {
					t.Errorf("%s imports %s directly; go through internal/engine", pkg, imp)
				}
			}
		}
	}
	if listed < 2 {
		t.Errorf("go list ./... listed %d packages, want the server and internal/explanation", listed)
	}
}
