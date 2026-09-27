// Package metricstest verifies that a metrics.Extractor honors the contract
// defined in package metrics, in the style of testing/fstest. Every language
// extractor calls TestExtractor once from its own tests instead of writing
// its own golden harness, and consumers that need an Extractor without a real
// module (estimate, gate, CLI) use NewFake.
package metricstest

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// unknownPackage is a package identifier no extractor should accept.
const unknownPackage = "metricstest.invalid/no-such-package"

// Fixture describes the module an implementation hands to TestExtractor.
// Build one per language fixture, in the implementation's conformance test.
type Fixture struct {
	// Root is the module root the extractor must Detect and list packages
	// under. It becomes ModuleContext.Root for every Extract call.
	Root string
	// ModulePath is the prefix trimmed from each package identifier to name
	// its golden file; see GoldenDir. Leave it empty when the identifiers are
	// already module-relative.
	ModulePath string
	// Packages is the complete, sorted list of package identifiers Packages
	// must return for Root. The cross-package invariants assume it covers the
	// whole module.
	Packages []string
	// GoldenDir holds one golden per entry of Packages, in the RawMetrics
	// JSON encoding with v1 fields omitted or null, plus the COUNTING.md that
	// explains how each value was derived. It is required. A golden is named
	// by the package's module-relative path: ModulePath + "/" is trimmed from
	// the identifier, each remaining "/" is a subdirectory, and ".json" is
	// appended, so "example.com/m/nested/pkg" with ModulePath "example.com/m"
	// is nested/pkg.json. The identifier equal to ModulePath, the module's
	// root package, is root.json. An empty ModulePath trims nothing.
	GoldenDir string
	// Update makes the golden subtest rewrite every golden from the
	// extractor's output instead of comparing against the files on disk. Set
	// it only from an explicit -update flag in the implementation's test,
	// after an intentional counting change recorded in COUNTING.md; the
	// default false never writes.
	Update bool
}

// TestExtractor runs the metrics.Extractor contract against ext using fx.
// An implementation calls it exactly once, from a test in its own package,
// and keeps only language-specific unit tests locally. It checks Detect,
// Packages, Validate on every Extract result, byte-identical determinism
// across two module contexts, errors for an unknown package and a cancelled
// context, module-wide invariants, and every golden in fx.GoldenDir. When ext
// also implements the optional metrics.Detailer, it checks that Details
// succeeds for every package after Extract and names as many untested
// exports as untested_exports counts.
//
// Implementations must check ctx.Err() at least once on every Extract call,
// including calls served from ModuleContext.Cache, so that a cancelled
// context always yields an error.
func TestExtractor(t *testing.T, ext metrics.Extractor, fx Fixture) {
	t.Helper()
	if len(fx.Packages) == 0 {
		t.Fatal("metricstest: Fixture.Packages is empty; list every package of the fixture module")
	}
	if fx.GoldenDir == "" {
		t.Fatal("metricstest: Fixture.GoldenDir is empty; goldens are part of the contract")
	}

	t.Run("Detect", func(t *testing.T) {
		if !ext.Detect(fx.Root) {
			t.Errorf("Detect(%s) = false, want true", fx.Root)
		}
		if empty := t.TempDir(); ext.Detect(empty) {
			t.Errorf("Detect(%s) = true on an empty directory, want false", empty)
		}
	})

	t.Run("Packages", func(t *testing.T) { checkPackages(t, ext, fx) })

	// Extract every package once with one shared module context, as a real
	// invocation would. The results feed the remaining subtests.
	mod := &metrics.ModuleContext{Root: fx.Root}
	got := make(map[string]metrics.RawMetrics, len(fx.Packages))
	ok := t.Run("Extract", func(t *testing.T) {
		for _, pkg := range fx.Packages {
			m, err := ext.Extract(t.Context(), mod, pkg)
			if err != nil {
				t.Errorf("Extract(%s): %v", pkg, err)
				continue
			}
			got[pkg] = m
		}
	})
	if !ok {
		t.Fatal("metricstest: Extract failed; later subtests need every package")
	}

	t.Run("Extract/Validate", func(t *testing.T) {
		for _, pkg := range fx.Packages {
			m := got[pkg]
			if err := m.Validate(); err != nil {
				t.Errorf("%s: %v", pkg, err)
			}
		}
	})

	t.Run("Extract/Deterministic", func(t *testing.T) {
		checkDeterministic(t, ext, fx, got)
	})

	t.Run("Extract/Unknown", func(t *testing.T) {
		if _, err := ext.Extract(t.Context(), mod, unknownPackage); err == nil {
			t.Errorf("Extract(%s) returned no error for an unknown package", unknownPackage)
		}
	})

	t.Run("Extract/Cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		pkg := fx.Packages[0]
		if _, err := ext.Extract(ctx, &metrics.ModuleContext{Root: fx.Root}, pkg); err == nil {
			t.Errorf("Extract(%s) with a cancelled context and a fresh module context returned no error", pkg)
		}
		if _, err := ext.Extract(ctx, mod, pkg); err == nil {
			t.Errorf("Extract(%s) with a cancelled context and a warm module context returned no error", pkg)
		}
	})

	t.Run("Invariants", func(t *testing.T) { checkInvariants(t, fx.Packages, got) })

	if d, ok := ext.(metrics.Detailer); ok {
		t.Run("Details", func(t *testing.T) { checkDetails(t, d, mod, fx.Packages, got) })
	}

	t.Run("Goldens", func(t *testing.T) { checkGoldens(t, fx, got) })
}

func checkPackages(t *testing.T, ext metrics.Extractor, fx Fixture) {
	t.Helper()
	first, err := ext.Packages(fx.Root)
	if err != nil {
		t.Fatalf("Packages(%s): %v", fx.Root, err)
	}
	if !slices.IsSorted(first) {
		t.Errorf("Packages(%s) = %v, not sorted", fx.Root, first)
	}
	if !slices.Equal(first, fx.Packages) {
		t.Errorf("Packages(%s) = %v, want %v", fx.Root, first, fx.Packages)
	}
	second, err := ext.Packages(fx.Root)
	if err != nil {
		t.Fatalf("Packages(%s) second call: %v", fx.Root, err)
	}
	if !slices.Equal(first, second) {
		t.Errorf("Packages(%s) is unstable: first %v, then %v", fx.Root, first, second)
	}
}

// checkDeterministic re-extracts every package with a fresh module context
// and requires the JSON encoding to match the first run byte for byte.
func checkDeterministic(t *testing.T, ext metrics.Extractor, fx Fixture, got map[string]metrics.RawMetrics) {
	t.Helper()
	mod := &metrics.ModuleContext{Root: fx.Root}
	for _, pkg := range fx.Packages {
		again, err := ext.Extract(t.Context(), mod, pkg)
		if err != nil {
			t.Errorf("Extract(%s) second run: %v", pkg, err)
			continue
		}
		a, errA := json.Marshal(got[pkg])
		b, errB := json.Marshal(again)
		if errA != nil || errB != nil {
			t.Errorf("%s: encoding metrics: %v, %v", pkg, errA, errB)
			continue
		}
		if string(a) != string(b) {
			t.Errorf("%s: Extract is not deterministic:\nfirst:  %s\nsecond: %s", pkg, a, b)
		}
	}
}

// checkInvariants checks relations that hold for any correct extractor and
// need no golden. Fan-in and fan-out are two views of the same internal
// import edges, so their module-wide sums agree, and the coupling ratios
// are consistent and free of float noise (see checkRatios).
func checkInvariants(t *testing.T, pkgs []string, got map[string]metrics.RawMetrics) {
	t.Helper()
	var fanIn, fanOut int
	for _, pkg := range pkgs {
		m := got[pkg]
		fanIn += m.FanIn
		fanOut += m.InternalImports
		if m.HasTests != (m.TestFuncs > 0) {
			t.Errorf("%s: has_tests %v disagrees with test_funcs %d", pkg, m.HasTests, m.TestFuncs)
		}
		if m.LargestFileSLOC > m.SLOC {
			t.Errorf("%s: largest_file_sloc %d exceeds sloc %d", pkg, m.LargestFileSLOC, m.SLOC)
		}
		if m.TokensEstWithTests < m.TokensEst {
			t.Errorf("%s: tokens_est_with_tests %d is below tokens_est %d", pkg, m.TokensEstWithTests, m.TokensEst)
		}
		checkRatios(t, pkg, m)
	}
	if fanIn != fanOut {
		t.Errorf("module-wide sum(fan_in) %d != sum(internal_imports) %d; each internal import edge must count once on each side",
			fanIn, fanOut)
	}
}

// ratioDecimals is the number of decimal places the coupling ratios
// instability, abstractness and main_sequence_distance are reported to.
const ratioDecimals = 3

// ratioTolerance bounds how far a reported ratio may sit from the value
// recomputed from its unrounded inputs: half a unit in the last place for
// the ratio itself and for each of the two ratios main_sequence_distance is
// computed from.
const ratioTolerance = 1.5e-3 + 1e-9

// checkRatios checks the coupling ratios of m that are reported (non-nil):
// each is rounded to ratioDecimals places, so an exact 0 or 1 carries no
// float noise; instability agrees with fan_in and internal_imports, which
// must not both be 0; and main_sequence_distance agrees with the other two,
// which must be reported with it.
func checkRatios(t *testing.T, pkg string, m metrics.RawMetrics) {
	t.Helper()
	scale := math.Pow10(ratioDecimals)
	for _, r := range []struct {
		name string
		v    *float64
	}{
		{"instability", m.Instability},
		{"abstractness", m.Abstractness},
		{"main_sequence_distance", m.MainSequenceDistance},
	} {
		if r.v != nil && math.Round(*r.v*scale)/scale != *r.v {
			t.Errorf("%s: %s %v is not rounded to %d decimals", pkg, r.name, *r.v, ratioDecimals)
		}
	}
	if m.Instability != nil {
		edges := m.FanIn + m.InternalImports
		if edges == 0 {
			t.Errorf("%s: instability %v is reported with fan_in and internal_imports both 0", pkg, *m.Instability)
		} else if want := float64(m.InternalImports) / float64(edges); math.Abs(*m.Instability-want) > ratioTolerance {
			t.Errorf("%s: instability %v, want internal_imports/(fan_in+internal_imports) = %d/%d",
				pkg, *m.Instability, m.InternalImports, edges)
		}
	}
	if d := m.MainSequenceDistance; d != nil {
		a, i := m.Abstractness, m.Instability
		if a == nil || i == nil {
			t.Errorf("%s: main_sequence_distance %v is reported without abstractness and instability", pkg, *d)
		} else if want := math.Abs(*a + *i - 1); math.Abs(*d-want) > ratioTolerance {
			t.Errorf("%s: main_sequence_distance %v, want |abstractness %v + instability %v - 1|", pkg, *d, *a, *i)
		}
	}
}

// checkDetails runs only for an extractor that implements the optional
// metrics.Detailer. After Extract on mod, Details must succeed for every
// package and name exactly as many untested exports as untested_exports
// counts.
func checkDetails(t *testing.T, d metrics.Detailer, mod *metrics.ModuleContext, pkgs []string, got map[string]metrics.RawMetrics) {
	t.Helper()
	for _, pkg := range pkgs {
		det, err := d.Details(t.Context(), mod, pkg)
		if err != nil {
			t.Errorf("Details(%s): %v", pkg, err)
			continue
		}
		if n, want := len(det.UntestedExports), got[pkg].UntestedExports; n != want {
			t.Errorf("%s: Details names %d untested exports %q, want untested_exports %d",
				pkg, n, det.UntestedExports, want)
		}
	}
}

func checkGoldens(t *testing.T, fx Fixture, got map[string]metrics.RawMetrics) {
	t.Helper()
	counting := filepath.Join(fx.GoldenDir, "COUNTING.md")
	for _, pkg := range fx.Packages {
		m := got[pkg]
		name := goldenName(fx.ModulePath, pkg)
		if fx.Update {
			if err := writeGolden(fx.GoldenDir, name, m); err != nil {
				t.Errorf("%s: %v", pkg, err)
			}
			continue
		}
		want, err := LoadGolden(fx.GoldenDir, name)
		if err != nil {
			t.Errorf("%s: %v", pkg, err)
			continue
		}
		for _, d := range diff(&want, &m) {
			t.Errorf("%s: %s: golden %s, got %s (check %s before regenerating)",
				pkg, d.field, d.golden, d.got, counting)
		}
	}
}

// rootGolden names the golden of the package whose identifier equals
// Fixture.ModulePath.
const rootGolden = "root"

// goldenName returns the module-relative golden name of package id: id with
// modulePath + "/" trimmed, or rootGolden when id is modulePath itself. An
// empty modulePath trims nothing.
func goldenName(modulePath, id string) string {
	if modulePath == "" {
		return id
	}
	if id == modulePath {
		return rootGolden
	}
	return strings.TrimPrefix(id, modulePath+"/")
}
