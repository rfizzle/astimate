package engine

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

// namedModuleTarget writes a module whose path is literally "module", so
// its root package's import path is the module row's old baseline key,
// and loads it with a logger writing to logs.
func namedModuleTarget(t *testing.T, logs *bytes.Buffer) *Target {
	t.Helper()
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"go.mod":   "module module\n\ngo 1.22\n",
		"root.go":  "package root\n\n// F is exported.\nfunc F() int { return 1 }\n",
		"sub/s.go": "package sub\n\n// G is exported.\nfunc G() int { return 2 }\n",
	})
	tg, err := LoadTarget(root, TargetOptions{Logger: slog.New(slog.NewTextHandler(logs, nil))})
	if err != nil {
		t.Fatalf("LoadTarget: %v", err)
	}
	return tg
}

// checkedRows returns the module row and the root package's row of c.
func checkedRows(t *testing.T, c *report.Check) (mod, rootPkg *report.Report) {
	t.Helper()
	if c.Module == nil {
		t.Fatal("check has no module row")
	}
	for i := range c.Packages {
		if c.Packages[i].Report.PackagePath == "." {
			rootPkg = &c.Packages[i].Report
		}
	}
	if rootPkg == nil {
		t.Fatalf("check has no root package row among %d packages", len(c.Packages))
	}
	return &c.Module.Report, rootPkg
}

// TestCheckModuleNamedModule checks a module whose path is "module": its
// root package and the module row are baselined and checked apart, and a
// baseline file that stores the module row under the old key is read with
// one warning.
func TestCheckModuleNamedModule(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	t.Run("reserved key", func(t *testing.T) {
		t.Parallel()
		var logs bytes.Buffer
		tg := namedModuleTarget(t, &logs)
		path, n, err := WriteBaseline(t.Context(), tg, "")
		if err != nil {
			t.Fatalf("WriteBaseline: %v", err)
		}
		if n != 2 {
			t.Errorf("WriteBaseline counted %d packages, want 2", n)
		}
		b, err := baseline.FromFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if m, ok := b.Metrics("module"); !ok || m.Files != 1 {
			t.Errorf("baseline row of package module = %+v (present %v), want the root package", m, ok)
		}
		if m, ok := b.Metrics(metrics.ModuleRowID); !ok || m.DupBlocksCrossPkg == nil || m.Files != 0 {
			t.Errorf("baseline module row = %+v (present %v), want the module row", m, ok)
		}

		// A new global in the root package fails it, not the module row.
		// The target is reloaded, since a target extracts its load.
		writeFiles(t, tg.Mod.Root, map[string]string{"state.go": "package root\n\nvar state int\n"})
		tg, err = LoadTarget(tg.Mod.Root, TargetOptions{Logger: tg.Logger})
		if err != nil {
			t.Fatalf("LoadTarget: %v", err)
		}
		c, failed, err := Check(t.Context(), tg, CheckOptions{BaselineFile: path, All: true})
		if err != nil || len(failed) > 0 {
			t.Fatalf("Check: err %v, failed %v", err, failed)
		}
		mod, rootPkg := checkedRows(t, c)
		if mod.PackagePath != metrics.ModuleRowID || mod.Baseline == nil || len(mod.Violations) != 0 {
			t.Errorf("module row = %s, baseline %v, violations %+v; want %s against its baseline row, passing",
				mod.PackagePath, mod.Baseline, mod.Violations, metrics.ModuleRowID)
		}
		if rootPkg.Baseline == nil || len(rootPkg.Violations) != 1 || rootPkg.Violations[0].Metric != "globals" {
			t.Errorf("root package baseline %v, violations %+v; want one globals violation against its own row",
				rootPkg.Baseline, rootPkg.Violations)
		}
		if strings.Contains(logs.String(), "old key") {
			t.Errorf("reading a new baseline file logged the migration:\n%s", logs.String())
		}
	})

	t.Run("old key", func(t *testing.T) {
		t.Parallel()
		var logs bytes.Buffer
		tg := namedModuleTarget(t, &logs)
		// A file written before the key was reserved held the module row
		// under "module", where it overwrote the root package's row.
		cross := 0
		old, err := json.Marshal(map[string]any{
			"ref": "", "module_path": "module", "tokenizer": TokenizerEst,
			"packages": map[string]metrics.RawMetrics{
				"module/sub": {Files: 1},
				"module":     {DupBlocksCrossPkg: &cross},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "baseline.json")
		if err := os.WriteFile(path, old, 0o600); err != nil {
			t.Fatal(err)
		}
		c, failed, err := Check(t.Context(), tg, CheckOptions{BaselineFile: path, All: true})
		if err != nil || len(failed) > 0 {
			t.Fatalf("Check: err %v, failed %v", err, failed)
		}
		mod, rootPkg := checkedRows(t, c)
		if mod.Baseline == nil {
			t.Error("module row has no baseline, want the row read from the old key")
		}
		if rootPkg.Baseline != nil {
			t.Errorf("root package baseline = %+v, want none: the old file lost its row", rootPkg.Baseline)
		}
		if got := strings.Count(logs.String(), `under the old key \"module\"`); got != 1 {
			t.Errorf("logged the migration %d times, want once:\n%s", got, logs.String())
		}
	})
}
