package metricstest

import (
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// checkDetailsConsistency checks the parts of det, the details of pkg, that
// mirror counts in m: positions recorded index for index with the untested
// exports and one per global, and the cross-package blocks behind
// dup_blocks_cross_pkg. Nil positions or blocks mean the implementation
// does not record them and are not checked.
func checkDetailsConsistency(t *testing.T, pkg string, det metrics.Details, m metrics.RawMetrics, pkgs []string) {
	t.Helper()
	if det.UntestedPositions != nil && len(det.UntestedPositions) != len(det.UntestedExports) {
		t.Errorf("%s: Details has %d untested positions for %d untested exports %q, want one per export",
			pkg, len(det.UntestedPositions), len(det.UntestedExports), det.UntestedExports)
	}
	if det.GlobalPositions != nil && len(det.GlobalPositions) != m.Globals {
		t.Errorf("%s: Details has %d global positions, want globals %d", pkg, len(det.GlobalPositions), m.Globals)
	}
	if det.CrossBlocks == nil {
		return
	}
	if n := m.DupBlocksCrossPkg; n != nil && len(det.CrossBlocks) != *n {
		t.Errorf("%s: Details names %d cross-package blocks, want dup_blocks_cross_pkg %d", pkg, len(det.CrossBlocks), *n)
	}
	checkCrossBlocks(t, pkg, det.CrossBlocks, pkgs, pkg)
}

// checkCrossBlocks checks each block of blocks, reported for the row id:
// it has at least two occurrences in at least two distinct packages, all
// among pkgs, one of them self unless self is empty; every file is a clean
// slash-separated path relative to the module root; and every line range
// is positive with start <= end.
func checkCrossBlocks(t *testing.T, id string, blocks []metrics.CrossBlock, pkgs []string, self string) {
	t.Helper()
	for i, b := range blocks {
		touched := make(map[string]bool, len(b.Occurrences))
		for _, o := range b.Occurrences {
			touched[o.Package] = true
			if !slices.Contains(pkgs, o.Package) {
				t.Errorf("%s: cross-package block %d has an occurrence in %q, want a package Packages lists", id, i, o.Package)
			}
			if !isRelSlashPath(o.File) {
				t.Errorf("%s: cross-package block %d has an occurrence in file %q, want a clean slash-separated path relative to the module root",
					id, i, o.File)
			}
			if o.StartLine < 1 || o.EndLine < o.StartLine {
				t.Errorf("%s: cross-package block %d has an occurrence at %s:%d-%d, want 1 <= start <= end",
					id, i, o.File, o.StartLine, o.EndLine)
			}
		}
		if len(b.Occurrences) < 2 || len(touched) < 2 {
			t.Errorf("%s: cross-package block %d has %d occurrences in %d packages, want at least two in at least two packages",
				id, i, len(b.Occurrences), len(touched))
		}
		if self != "" && !touched[self] {
			t.Errorf("%s: cross-package block %d has no occurrence in the package itself", id, i)
		}
	}
}

// isRelSlashPath reports whether f is a non-empty, clean, slash-separated
// path that is not absolute and does not climb out of its root.
func isRelSlashPath(f string) bool {
	return f != "" && !strings.Contains(f, `\`) && path.Clean(f) == f &&
		!path.IsAbs(f) && !filepath.IsAbs(filepath.FromSlash(f)) &&
		f != ".." && !strings.HasPrefix(f, "../")
}

// checkModuleDetails runs only for an extractor that implements
// metrics.ModuleDetailer and whose module row reports dup_blocks_cross_pkg.
// ModuleDetails must name exactly that many valid blocks, and when the
// extractor is also a metrics.Detailer, each block must be among the
// CrossBlocks of every package it touches.
func checkModuleDetails(t *testing.T, ext metrics.Extractor, md metrics.ModuleDetailer, mod *metrics.ModuleContext,
	pkgs []string, row *metrics.RawMetrics,
) {
	t.Helper()
	if row.DupBlocksCrossPkg == nil {
		return // reported by the shape check
	}
	det, err := md.ModuleDetails(t.Context(), mod)
	if err != nil {
		t.Fatalf("ModuleDetails: %v", err)
	}
	if n, want := len(det.CrossBlocks), *row.DupBlocksCrossPkg; n != want {
		t.Errorf("%s: ModuleDetails names %d cross-package blocks, want dup_blocks_cross_pkg %d",
			metrics.ModuleRowID, n, want)
	}
	checkCrossBlocks(t, metrics.ModuleRowID, det.CrossBlocks, pkgs, "")
	d, ok := ext.(metrics.Detailer)
	if !ok {
		return
	}
	perPkg := make(map[string][]metrics.CrossBlock, len(pkgs))
	for i, b := range det.CrossBlocks {
		checked := make(map[string]bool, len(b.Occurrences))
		for _, o := range b.Occurrences {
			if checked[o.Package] || !slices.Contains(pkgs, o.Package) {
				continue // an unknown package is reported above
			}
			checked[o.Package] = true
			blocks, seen := perPkg[o.Package]
			if !seen {
				pd, err := d.Details(t.Context(), mod, o.Package)
				if err != nil {
					t.Errorf("Details(%s): %v", o.Package, err)
				}
				blocks = pd.CrossBlocks
				perPkg[o.Package] = blocks
			}
			if !slices.ContainsFunc(blocks, func(c metrics.CrossBlock) bool {
				return slices.Equal(c.Occurrences, b.Occurrences)
			}) {
				t.Errorf("%s: cross-package block %d touches %s, but Details(%s) does not list it",
					metrics.ModuleRowID, i, o.Package, o.Package)
			}
		}
	}
}
