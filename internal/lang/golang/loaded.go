package golang

import (
	"errors"
	"fmt"
	"sync"

	"github.com/rfizzle/astimate/internal/lang/duptok"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/dup"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/imports"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/inspect"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/tests"
	"golang.org/x/tools/go/packages"
)

// ErrNoPackages is returned when a module root contains no Go packages.
var ErrNoPackages = errors.New("no Go packages in module")

// loaded is one module load with the module-wide indexes built from it at
// most once. It is built once per module root and shared by every metric
// for every package in the module; ModuleContext.Cache holds it.
type loaded struct {
	*load.Module
	// graph is the reverse import graph behind fan_in and fan_in_tests,
	// built on first use.
	graph imports.Graph
	// testRefs is the module-wide test reference index behind
	// untested_exports: what the test files of every package refer to,
	// built on first use. A metric that needs "referenced from a test
	// anywhere in the module" reads it.
	testRefs tests.Refs
	// cross memoizes the cross-package duplication counts and block
	// locations behind dup_blocks_cross_pkg, per duplication options, built
	// once per load on first use. It holds counts and line ranges only,
	// never file contents.
	cross dup.Memo

	// detailsMu guards details, which maps an import path to the debug
	// details of its most recent Extract. Besides graph, testRefs and cross
	// it is the only state on loaded written after the load.
	detailsMu sync.Mutex
	details   map[string]details
}

// details is the per-package debug record the metric functions produce
// beside the counts: the names and declarations behind untested_exports
// and globals, the duplicate block locations, the blank and dot imports,
// the token counting method, and every function's complexity and
// fingerprint for the baseline diff.
type details struct {
	untested     tests.UntestedCounts
	globals      inspect.GlobalCounts
	imp          imports.Counts
	dupLocations []duptok.Location
	tokensMethod string
	functions    []inspect.Func
	// largestFile is the absolute filename of the largest non-test file.
	largestFile string
	// files are the absolute filenames of the non-test files.
	files []string
}

// loadModule loads the module at cfg.Dir, which must be an absolute module
// root, with load.Load, and fails with ErrNoPackages when it has no
// packages.
func loadModule(cfg *packages.Config, fn load.Func) (*loaded, error) {
	m, err := load.Load(cfg, fn)
	if err != nil {
		return nil, err
	}
	if len(m.Paths) == 0 {
		return nil, fmt.Errorf("loading %s: %w", cfg.Dir, ErrNoPackages)
	}
	return &loaded{Module: m}, nil
}

// setDetails records d as the most recent details of the package at
// importPath.
func (l *loaded) setDetails(importPath string, d details) {
	l.detailsMu.Lock()
	defer l.detailsMu.Unlock()
	if l.details == nil {
		l.details = make(map[string]details)
	}
	l.details[importPath] = d
}

// detailsOf returns the most recent details recorded for the package at
// importPath, and whether any were.
func (l *loaded) detailsOf(importPath string) (details, bool) {
	l.detailsMu.Lock()
	defer l.detailsMu.Unlock()
	d, ok := l.details[importPath]
	return d, ok
}
