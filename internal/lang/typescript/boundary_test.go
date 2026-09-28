package typescript

import (
	"os/exec"
	"strings"
	"testing"
)

// TestImportBoundary checks that the extractor and its internal packages
// stay below the gate: nothing under internal/lang/typescript depends on
// engine, report, gate, score, config or baseline, directly or not.
func TestImportBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	t.Parallel()

	out, err := exec.CommandContext(t.Context(), "go", "list", "-deps", "-f", "{{.ImportPath}}", "./...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	deps := strings.Fields(string(out))
	for _, forbidden := range []string{"engine", "report", "gate", "score", "config", "baseline"} {
		for _, dep := range deps {
			if strings.HasSuffix(dep, "/astimate/internal/"+forbidden) {
				t.Errorf("internal/lang/typescript/... depends on %s", dep)
			}
		}
	}
	for _, sub := range []string{"resolve", "inspect", "walk"} {
		if !strings.Contains(string(out), "/internal/lang/typescript/internal/"+sub+"\n") {
			t.Errorf("go list ./... does not list internal/%s; the check would pass vacuously", sub)
		}
	}
}
