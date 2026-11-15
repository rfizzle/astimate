package extract

import (
	"sync"

	"github.com/rfizzle/astimate/internal/lang/duptok"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/dup"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/imports"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/inspect"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/tests"
)

// Module is one module load with the module-wide indexes built from it at
// most once. It is built once per module root and shared by every metric
// for every package in the module; ModuleContext.Cache holds it.
type Module struct {
	*load.Module
	// Root is the absolute module root the load was made from; empty for
	// the standard-library loads.
	Root string
	// Graph is the reverse import graph behind fan_in and fan_in_tests,
	// built on first use.
	Graph imports.Graph
	// CrossDup memoizes the cross-package duplication counts and block
	// locations behind dup_blocks_cross_pkg, per duplication options, built
	// once per load on first use. It holds counts and line ranges only,
	// never file contents.
	CrossDup dup.Memo
	// testRefs is the module-wide test reference index behind
	// untested_exports: what the test files of every package refer to,
	// built on first use. A metric that needs "referenced from a test
	// anywhere in the module" reads it.
	testRefs tests.Refs

	// detailsMu guards details, which maps an import path to the debug
	// details of its most recent Assemble. Besides Graph, testRefs and
	// CrossDup it is the only state on Module written after the load.
	detailsMu sync.Mutex
	details   map[string]Details
}

// Details is the per-package debug record the metric functions produce
// beside the counts: the names and declarations behind untested_exports
// and globals, the duplicate blocks with their locations, the blank and
// dot imports,
// the token counting method, and every function's complexity and
// fingerprint for the baseline diff.
type Details struct {
	Untested     tests.UntestedCounts
	Globals      inspect.GlobalCounts
	Imports      imports.Counts
	DupBlocks    []duptok.Group
	TokensMethod string
	Functions    []inspect.Func
	// Exports are the package's top-level declarations.
	Exports inspect.ExportCounts
	// LargestFile is the absolute filename of the largest non-test file.
	LargestFile string
	// Files are the absolute filenames of the non-test files.
	Files []string
	// Covered names the exports of Untested whose body the last coverage
	// run executed (SetCovered); nil before one.
	Covered []string
}

// New returns the module context of m, a load made from the absolute
// module root root, with every module-wide index still unbuilt.
func New(m *load.Module, root string) *Module {
	return &Module{Module: m, Root: root}
}

// setDetails records d as the most recent details of the package at
// importPath.
func (m *Module) setDetails(importPath string, d Details) {
	m.detailsMu.Lock()
	defer m.detailsMu.Unlock()
	if m.details == nil {
		m.details = make(map[string]Details)
	}
	m.details[importPath] = d
}

// DetailsOf returns the most recent details recorded for the package at
// importPath, and whether any were.
func (m *Module) DetailsOf(importPath string) (Details, bool) {
	m.detailsMu.Lock()
	defer m.detailsMu.Unlock()
	d, ok := m.details[importPath]
	return d, ok
}

// SetCovered records covered, the untested exports a coverage run
// executed, with the details of the package at importPath; it does
// nothing when no details are recorded for it.
func (m *Module) SetCovered(importPath string, covered []string) {
	m.detailsMu.Lock()
	defer m.detailsMu.Unlock()
	if d, ok := m.details[importPath]; ok {
		d.Covered = covered
		m.details[importPath] = d
	}
}

// ForgetDetails drops every recorded details, so their memory can be
// reclaimed while the load itself is still held; a later Assemble records
// afresh.
func (m *Module) ForgetDetails() {
	m.detailsMu.Lock()
	defer m.detailsMu.Unlock()
	m.details = nil
}
