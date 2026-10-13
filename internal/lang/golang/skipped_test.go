package golang

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// TestSkippedPackages loads testdata/go/skipped, whose package remote
// imports a module go.mod does not require, and checks that Packages lists
// the rest, Skipped names remote with its reason, Extract of remote says
// why it is unknown, the log names remote relative to the module, and
// base's fan_in still counts remote's import.
func TestSkippedPackages(t *testing.T) {
	t.Parallel()

	root := filepath.Join(filepath.Dir(fixtureRoot(t)), "skipped")
	var logs bytes.Buffer
	e := New(WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))

	paths, err := e.Packages(root)
	if err != nil {
		t.Fatalf("Packages: %v", err)
	}
	if want := []string{"example.com/skipped/app", "example.com/skipped/base"}; !slices.Equal(paths, want) {
		t.Errorf("Packages = %v, want %v", paths, want)
	}
	skipped, err := e.Skipped(root)
	if err != nil {
		t.Fatalf("Skipped: %v", err)
	}
	if len(skipped) != 1 || skipped[0].Package != "example.com/skipped/remote" || skipped[0].Reason != "missing module" ||
		!strings.Contains(skipped[0].Error, "example.com/absent/client") {
		t.Errorf("Skipped = %+v, want remote, missing module", skipped)
	}

	mod := &metrics.ModuleContext{Root: root, ModulePath: "example.com/skipped"}
	_, err = e.Extract(t.Context(), mod, "example.com/skipped/remote")
	if !errors.Is(err, metrics.ErrUnknownPackage) || !strings.Contains(err.Error(), "skipped, it does not load (missing module)") {
		t.Errorf("Extract of remote = %v, want an unknown package that says it was skipped", err)
	}
	m, err := e.Extract(t.Context(), mod, "example.com/skipped/base")
	if err != nil {
		t.Fatal(err)
	}
	if m.FanIn != 2 {
		t.Errorf("base fan_in = %d, want 2: app and the skipped remote", m.FanIn)
	}

	text := logs.String()
	if !strings.Contains(text, "package=example.com/skipped/remote") || !strings.Contains(text, filepath.Join("remote", "remote.go")) ||
		strings.Contains(text, root) {
		t.Errorf("log does not name remote with a module-relative error:\n%s", text)
	}
}

// TestRepeatLoadLogsOnce checks that the load of a copy of a module marked
// Repeat logs a skipped package once between the two loads, and that Forget
// of the copy ends the mark.
func TestRepeatLoadLogsOnce(t *testing.T) {
	t.Parallel()

	root := filepath.Join(filepath.Dir(fixtureRoot(t)), "skipped")
	copyRoot := t.TempDir()
	if err := os.CopyFS(copyRoot, os.DirFS(root)); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	e := New(WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))

	const line = "skipped package; it does not load"
	if _, err := e.Packages(root); err != nil {
		t.Fatal(err)
	}
	e.Repeat(copyRoot)
	if _, err := e.Packages(copyRoot); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(logs.String(), line); n != 1 {
		t.Errorf("two loads logged the skipped package %d times, want once:\n%s", n, logs.String())
	}
	e.Forget(copyRoot)
	if _, err := e.Packages(copyRoot); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(logs.String(), line); n != 2 {
		t.Errorf("a load after Forget logged %d times in all, want 2:\n%s", n, logs.String())
	}
}
