package metricstest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// moduleGolden names the golden of the module row, module.json in
// Fixture.GoldenDir. It is not the row's identifier, metrics.ModuleRowID,
// whose angle brackets make a poor file name.
const moduleGolden = "module"

// isModuleWide reports whether the v1 metric name is one the module row
// carries (SPEC.md sections 6 and 8.1). Every other v1 field of the row is
// null.
func isModuleWide(name string) bool {
	return name == "dup_blocks_cross_pkg"
}

// checkModuleRow runs the metrics.ModuleMetrics clauses against mm, which
// has extracted every package of fx into got using mod, and returns the row
// for the golden comparison, or nil when ModuleRow failed.
//
// The fixtures the suite runs on are modules, so the row always carries
// dup_blocks_cross_pkg; the null the Go extractor reports for a
// standard-library load, which is not a module, never reaches the suite.
func checkModuleRow(t *testing.T, mm metrics.ModuleMetrics, mod *metrics.ModuleContext, fx Fixture,
	got map[string]metrics.RawMetrics,
) *metrics.RawMetrics {
	t.Helper()
	var row *metrics.RawMetrics
	ok := t.Run("ModuleRow", func(t *testing.T) {
		m, err := mm.ModuleRow(t.Context(), mod)
		if err != nil {
			t.Fatalf("ModuleRow: %v", err)
		}
		row = &m
	})
	if !ok {
		return nil
	}

	t.Run("ModuleRow/Validate", func(t *testing.T) {
		if err := row.Validate(); err != nil {
			t.Errorf("%s: %v", metrics.ModuleRowID, err)
		}
		for _, pkg := range fx.Packages {
			if pkg == metrics.ModuleRowID {
				t.Errorf("package %s collides with the module row %q in a baseline", pkg, metrics.ModuleRowID)
			}
			if goldenName(fx.ModulePath, pkg) == moduleGolden {
				t.Errorf("package %s collides with the module row among the goldens: both are %s.json",
					pkg, moduleGolden)
			}
		}
	})

	t.Run("ModuleRow/Shape", func(t *testing.T) { checkModuleShape(t, row) })

	t.Run("ModuleRow/Deterministic", func(t *testing.T) {
		again, err := mm.ModuleRow(t.Context(), &metrics.ModuleContext{Root: fx.Root})
		if err != nil {
			t.Fatalf("ModuleRow with a fresh module context: %v", err)
		}
		a, errA := json.Marshal(row)
		b, errB := json.Marshal(again)
		if errA != nil || errB != nil {
			t.Fatalf("%s: encoding metrics: %v, %v", metrics.ModuleRowID, errA, errB)
		}
		if string(a) != string(b) {
			t.Errorf("%s: ModuleRow is not deterministic:\nfirst:  %s\nsecond: %s", metrics.ModuleRowID, a, b)
		}
	})

	t.Run("ModuleRow/Cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := mm.ModuleRow(ctx, &metrics.ModuleContext{Root: fx.Root}); err == nil {
			t.Errorf("ModuleRow with a cancelled context and a fresh module context returned no error")
		}
		if _, err := mm.ModuleRow(ctx, mod); err == nil {
			t.Errorf("ModuleRow with a cancelled context and a warm module context returned no error")
		}
	})

	t.Run("ModuleRow/Invariants", func(t *testing.T) { checkModuleSums(t, row, fx.Packages, got) })

	return row
}

// checkModuleShape checks the row's layout (SPEC.md section 10.2): every v0
// field is zero, and every v1 field is null except the module-wide ones,
// which are reported.
func checkModuleShape(t *testing.T, row *metrics.RawMetrics) {
	t.Helper()
	var zero metrics.RawMetrics
	for _, name := range metrics.MetricNames() {
		v, set := row.Value(name)
		if _, v0 := zero.Value(name); v0 {
			if v != 0 {
				t.Errorf("%s: %s is %s, want 0 on the module row", metrics.ModuleRowID, name, formatValue(v))
			}
			continue
		}
		switch {
		case isModuleWide(name) && !set:
			t.Errorf("%s: %s is null, want the module-wide value", metrics.ModuleRowID, name)
		case !isModuleWide(name) && set:
			t.Errorf("%s: %s is %s, want null: it is not module-wide", metrics.ModuleRowID, name, formatValue(v))
		}
	}
}

// checkModuleSums relates the row's dup_blocks_cross_pkg, the number of
// distinct cross-package blocks, to the per-package counts, where each block
// counts once in every package it touches. A block touches at least two
// packages and at most all of them, so the sum over packages lies between
// twice the row and the row times the number of packages. A null package
// count adds nothing.
func checkModuleSums(t *testing.T, row *metrics.RawMetrics, pkgs []string, got map[string]metrics.RawMetrics) {
	t.Helper()
	if row.DupBlocksCrossPkg == nil {
		return // reported by the shape check
	}
	blocks := *row.DupBlocksCrossPkg
	var sum int
	for _, pkg := range pkgs {
		if n := got[pkg].DupBlocksCrossPkg; n != nil {
			sum += *n
		}
	}
	if sum < 2*blocks {
		t.Errorf("module-wide sum(dup_blocks_cross_pkg) %d < 2 * module row %d; each block counts in at least two packages",
			sum, blocks)
	}
	if sum > blocks*len(pkgs) {
		t.Errorf("module-wide sum(dup_blocks_cross_pkg) %d > module row %d * %d packages; each block counts at most once per package",
			sum, blocks, len(pkgs))
	}
}
