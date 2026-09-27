package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
)

// writeFakeBaseline writes a baseline file for fakeTarget's module in a
// temporary directory and returns its path.
func writeFakeBaseline(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "baseline.json")
	pkgs := map[string]metrics.RawMetrics{"example.com/m/big": {TokensEst: 39000}}
	if err := baseline.Write(path, "", "example.com/m", pkgs); err != nil {
		t.Fatalf("writing baseline: %v", err)
	}
	return path
}

func TestCheckPackagesOverride(t *testing.T) {
	t.Parallel()

	tg := fakeTarget("")
	c, failed, err := Check(t.Context(), tg, CheckOptions{
		BaselineFile: writeFakeBaseline(t),
		Packages:     []string{"example.com/m/big"},
	})
	if err != nil || len(failed) != 0 {
		t.Fatalf("Check = (%v, %v), want no error", failed, err)
	}
	if len(c.Packages) != 1 || c.Packages[0].Report.PackagePath != "big" {
		t.Fatalf("checked %+v, want only big", c.Packages)
	}
	if c.Packages[0].Report.Baseline == nil {
		t.Error("big has no baseline block, want the file's metrics")
	}
	if c.Deleted != nil {
		t.Errorf("Deleted = %v, want none with an explicit package list", c.Deleted)
	}
}

func TestCheckNoBaseline(t *testing.T) {
	t.Parallel()

	// A module outside any git repository has no default ref and no
	// baseline file.
	root := t.TempDir()
	tg := &Target{
		Mod: &metrics.ModuleContext{Root: root, ModulePath: "example.com/m"},
		Ext: metricstest.NewFake("go", root, map[string]metrics.RawMetrics{"example.com/m": {}}),
		Cfg: &config.Config{Rebuild: rankParams()},
	}
	_, _, err := Check(t.Context(), tg, CheckOptions{Packages: []string{"example.com/m"}})
	if !errors.Is(err, ErrNoBaseline) {
		t.Fatalf("Check error = %v, want ErrNoBaseline", err)
	}
	if strings.Contains(err.Error(), "--base") {
		t.Errorf("Check error = %q, want no caller-specific hint", err)
	}
}

func TestBaselineCacheFile(t *testing.T) {
	t.Parallel()

	path := writeFakeBaseline(t)
	cache := NewBaselineCache()
	first, err := fileBaseline(path, cache)
	if err != nil {
		t.Fatal(err)
	}
	again, err := fileBaseline(path, cache)
	if err != nil {
		t.Fatal(err)
	}
	if again != first || cache.Loads() != 1 {
		t.Errorf("second read: same = %v, loads = %d, want the cached baseline and 1 load", again == first, cache.Loads())
	}

	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	edited, err := fileBaseline(path, cache)
	if err != nil {
		t.Fatal(err)
	}
	if edited == first || cache.Loads() != 2 {
		t.Errorf("after a touch: same = %v, loads = %d, want a fresh read and 2 loads", edited == first, cache.Loads())
	}

	var none *BaselineCache
	if _, err := fileBaseline(path, none); err != nil {
		t.Errorf("reading without a cache: %v", err)
	}
}
