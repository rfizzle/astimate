// Package metricstest verifies that a metrics.Extractor honors the contract
// defined in package metrics, in the style of testing/fstest. Every language
// extractor calls TestExtractor once from its own tests instead of writing
// its own golden harness, and consumers that need an Extractor without a real
// module (estimate, gate, CLI) use NewFake.
package metricstest

import (
	"context"
	"encoding/json"
	"errors"
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
	// root package, is root.json. An empty ModulePath trims nothing. When
	// the extractor implements metrics.ModuleMetrics, the directory also
	// holds module.json, the golden of the module row, so no package's
	// golden may be named module.
	GoldenDir string
	// Unmarked is optional: the root of a copy of the module at Root in
	// which every generated file has lost its generated-file marker (for
	// Go, the "// Code generated ... DO NOT EDIT." line), so the extractor
	// reads it as hand-written. The copy must list the same packages and
	// keep every file's length, defacing the marker rather than deleting
	// it, so the token counts of the two compare. When it is set,
	// TestExtractor extracts every package of the copy and checks that a
	// generated file adds nothing to the size and structure metrics; see
	// checkGenerated. Leave it empty for a language with no generated-file
	// convention or a fixture with no generated file.
	Unmarked string
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
// context, module-wide invariants, the generated-file invariants when
// fx.Unmarked is set, and every golden in fx.GoldenDir. When ext
// also implements the optional metrics.Detailer, it checks that Details
// succeeds for every package after Extract and names as many untested
// exports as untested_exports counts; that recorded positions number one
// per untested export and one per global; and that recorded CrossBlocks
// number dup_blocks_cross_pkg, each block with at least two occurrences in
// at least two packages of the fixture, the package itself among them,
// files slash-separated and relative, and line ranges positive with start
// <= end. When ext also implements the optional
// metrics.ImporterLister, it checks that Importers lists, for every package,
// sorted distinct packages of the fixture other than the package itself, as
// many as fan_in counts, and fails for an unknown package with
// metrics.ErrUnknownPackage. When ext also implements the optional
// metrics.ModuleMetrics, it checks the module row: it validates, no package
// collides with metrics.ModuleRowID, its v0 fields are zero and its v1
// fields null except the module-wide dup_blocks_cross_pkg, it is
// deterministic and fails on a cancelled context, the sum of
// dup_blocks_cross_pkg over packages lies between twice the row's value and
// the row's value times the number of packages, and it matches module.json
// in fx.GoldenDir. When ext also implements metrics.ModuleDetailer,
// ModuleDetails must name exactly the row's dup_blocks_cross_pkg blocks,
// each valid as above and, for a Detailer, listed in the Details of every
// package it touches. An extractor without ModuleMetrics skips those
// checks.
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

	if fx.Unmarked != "" {
		t.Run("Generated", func(t *testing.T) {
			unmarked := make(map[string]metrics.RawMetrics, len(fx.Packages))
			umod := &metrics.ModuleContext{Root: fx.Unmarked}
			for _, pkg := range fx.Packages {
				m, err := ext.Extract(t.Context(), umod, pkg)
				if err != nil {
					t.Errorf("Extract(%s) in the unmarked copy: %v", pkg, err)
					continue
				}
				unmarked[pkg] = m
			}
			checkGenerated(t, fx.Packages, got, unmarked)
		})
	}

	if d, ok := ext.(metrics.Detailer); ok {
		t.Run("Details", func(t *testing.T) { checkDetails(t, d, mod, fx.Packages, got) })
	}

	if il, ok := ext.(metrics.ImporterLister); ok {
		t.Run("Importers", func(t *testing.T) { checkImporters(t, il, mod, fx.Packages, got) })
	}

	// The module row is optional: an extractor without ModuleMetrics, such
	// as the TypeScript one, has none and skips its clauses and golden.
	var row *metrics.RawMetrics
	mm, hasRow := ext.(metrics.ModuleMetrics)
	if hasRow {
		row = checkModuleRow(t, mm, mod, fx, got)
		if md, ok := ext.(metrics.ModuleDetailer); ok && row != nil {
			t.Run("ModuleRow/Details", func(t *testing.T) { checkModuleDetails(t, ext, md, mod, fx.Packages, row) })
		}
	} else {
		t.Logf("metricstest: %s extractor does not implement metrics.ModuleMetrics; skipping the module row", ext.Language())
	}

	t.Run("Goldens", func(t *testing.T) {
		checkGoldens(t, fx, got)
		if row != nil {
			checkGolden(t, fx, metrics.ModuleRowID, moduleGolden, *row)
		}
	})
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
// import edges, so their module-wide sums agree; generated tokens need a
// generated file; and the coupling ratios are consistent and free of float
// noise (see checkRatios).
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
		if optCount(m.TokensEstGenerated) > 0 && optCount(m.GeneratedFiles) == 0 {
			t.Errorf("%s: tokens_est_generated %d without a generated file", pkg, *m.TokensEstGenerated)
		}
		checkRatios(t, pkg, m)
	}
	if fanIn != fanOut {
		t.Errorf("module-wide sum(fan_in) %d != sum(internal_imports) %d; each internal import edge must count once on each side",
			fanIn, fanOut)
	}
}

// generatedExcluded returns the size and structure metrics that count only
// the files a person wrote (SPEC.md 6.5) and can only grow when a file is
// counted too: sums, and maxima over files or functions. The token counts,
// which the generated volume moves between, are checked separately, and
// cognitive_p90, dup_blocks and duplication_pct are left to the goldens,
// since one more file can lower a percentile, merge blocks or dilute a
// share; see checkGenerated.
func generatedExcluded() []string {
	return []string{
		"sloc", "largest_file_sloc", "exported_symbols", "globals", "init_funcs", "max_nesting",
		"cognitive_total", "func_count", "untested_exports",
	}
}

// generatedCounted returns the metrics that read generated files like any
// other: files, which counts them, and the import metrics, since generated
// code imports real packages. Test metrics never see a non-test file.
func generatedCounted() []string {
	return []string{
		"files", "internal_imports", "external_imports", "stdlib_imports", "fan_in", "fan_in_tests",
		"test_files", "test_funcs", "has_tests",
	}
}

// checkGenerated compares every package as extracted (marked) with the same
// package in the unmarked copy of the module, where no file is generated.
// The copy must report no generated file and no generated tokens. A
// package with no generated file must be identical in both, except for
// dup_blocks_cross_pkg, which another package's file can move. A package with
// one must agree on the metrics in generatedCounted, and its generated
// volume must move into tokens_est and tokens_est_with_tests once the
// marker is gone: unmarked tokens_est is marked tokens_est plus marked
// tokens_est_generated, and likewise with tests, within one token for the
// truncation of a ratio estimate. Every metric in generatedExcluded must be
// at least as large unmarked, since the unmarked copy counts the same files
// plus the formerly generated ones; with the token identity this is the
// black-box form of "a generated file adds nothing to the excluded
// metrics", which the goldens then pin exactly.
func checkGenerated(t *testing.T, pkgs []string, marked, unmarked map[string]metrics.RawMetrics) {
	t.Helper()
	for _, pkg := range pkgs {
		m, u := marked[pkg], unmarked[pkg]
		if n := optCount(u.GeneratedFiles); n != 0 {
			t.Errorf("%s: unmarked copy reports generated_files %d, want 0", pkg, n)
		}
		if n := optCount(u.TokensEstGenerated); n != 0 {
			t.Errorf("%s: unmarked copy reports tokens_est_generated %d, want 0", pkg, n)
		}
		if optCount(m.GeneratedFiles) == 0 {
			// Another package's formerly generated file joins the module-wide
			// duplication stream, so only dup_blocks_cross_pkg may move.
			m.DupBlocksCrossPkg = u.DupBlocksCrossPkg
			a, errA := json.Marshal(m)
			b, errB := json.Marshal(u)
			if errA != nil || errB != nil {
				t.Errorf("%s: encoding metrics: %v, %v", pkg, errA, errB)
			} else if string(a) != string(b) {
				t.Errorf("%s: no generated file, yet the unmarked copy differs:\nmarked:   %s\nunmarked: %s", pkg, a, b)
			}
			continue
		}
		for _, name := range generatedCounted() {
			mv, _ := m.Value(name)
			uv, _ := u.Value(name)
			if mv != uv {
				t.Errorf("%s: %s %v, unmarked %v: generated files count in it like any other", pkg, name, mv, uv)
			}
		}
		for _, name := range generatedExcluded() {
			mv, _ := m.Value(name)
			uv, _ := u.Value(name)
			if mv > uv {
				t.Errorf("%s: %s %v exceeds unmarked %v: a generated file cannot add to it", pkg, name, mv, uv)
			}
		}
		gen := optCount(m.TokensEstGenerated)
		for _, tok := range []struct {
			name           string
			marked, unmark int
		}{
			{"tokens_est", m.TokensEst, u.TokensEst},
			{"tokens_est_with_tests", m.TokensEstWithTests, u.TokensEstWithTests},
		} {
			if d := tok.unmark - (tok.marked + gen); d < -1 || d > 1 {
				t.Errorf("%s: unmarked %s %d, want %s %d + tokens_est_generated %d (within 1)",
					pkg, tok.name, tok.unmark, tok.name, tok.marked, gen)
			}
		}
	}
}

// optCount returns *p, or 0 for nil.
func optCount(p *int) int {
	if p == nil {
		return 0
	}
	return *p
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
// counts; see checkDetailsConsistency for the positions and cross-package
// blocks it checks when they are present.
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
		checkDetailsConsistency(t, pkg, det, got[pkg], pkgs)
	}
}

// checkImporters checks that il lists, for every package, sorted distinct
// packages of pkgs other than the package itself, as many as its fan_in,
// and rejects an unknown package with metrics.ErrUnknownPackage.
func checkImporters(t *testing.T, il metrics.ImporterLister, mod *metrics.ModuleContext, pkgs []string, got map[string]metrics.RawMetrics) {
	t.Helper()
	for _, pkg := range pkgs {
		imp, err := il.Importers(t.Context(), mod, pkg)
		if err != nil {
			t.Errorf("Importers(%s): %v", pkg, err)
			continue
		}
		if !slices.IsSorted(imp) || len(slices.Compact(slices.Clone(imp))) != len(imp) {
			t.Errorf("Importers(%s) = %q, want sorted and distinct", pkg, imp)
		}
		for _, from := range imp {
			if from == pkg || !slices.Contains(pkgs, from) {
				t.Errorf("Importers(%s) lists %q, want another package of the fixture", pkg, from)
			}
		}
		if n, want := len(imp), got[pkg].FanIn; n != want {
			t.Errorf("%s: Importers lists %d packages %q, want fan_in %d", pkg, n, imp, want)
		}
	}
	if _, err := il.Importers(t.Context(), mod, unknownPackage); !errors.Is(err, metrics.ErrUnknownPackage) {
		t.Errorf("Importers(%s) error = %v, want metrics.ErrUnknownPackage", unknownPackage, err)
	}
}

func checkGoldens(t *testing.T, fx Fixture, got map[string]metrics.RawMetrics) {
	t.Helper()
	for _, pkg := range fx.Packages {
		checkGolden(t, fx, pkg, goldenName(fx.ModulePath, pkg), got[pkg])
	}
}

// checkGolden compares m, the metrics of the row id, against the golden
// named name in fx.GoldenDir, or rewrites that golden when fx.Update is set.
func checkGolden(t *testing.T, fx Fixture, id, name string, m metrics.RawMetrics) {
	t.Helper()
	if fx.Update {
		if err := writeGolden(fx.GoldenDir, name, m); err != nil {
			t.Errorf("%s: %v", id, err)
		}
		return
	}
	want, err := LoadGolden(fx.GoldenDir, name)
	if err != nil {
		t.Errorf("%s: %v", id, err)
		return
	}
	counting := filepath.Join(fx.GoldenDir, "COUNTING.md")
	for _, d := range diff(&want, &m) {
		t.Errorf("%s: %s: golden %s, got %s (check %s before regenerating)",
			id, d.field, d.golden, d.got, counting)
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
