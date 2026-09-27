package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestImportBoundary checks that the command reaches baseline, gate and
// score only through internal/engine, never by importing them directly.
func TestImportBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	t.Parallel()

	out, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Imports "\n"}}`, ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	imports := strings.Fields(string(out))
	for _, forbidden := range []string{"internal/baseline", "internal/gate", "internal/score"} {
		for _, imp := range imports {
			if strings.HasSuffix(imp, "/"+forbidden) {
				t.Errorf("cmd/astimate imports %s directly; go through internal/engine", imp)
			}
		}
	}
	engineImported := false
	for _, imp := range imports {
		engineImported = engineImported || strings.HasSuffix(imp, "/internal/engine")
	}
	if !engineImported {
		t.Errorf("cmd/astimate imports %q, want internal/engine among them", imports)
	}
}
