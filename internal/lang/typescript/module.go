package typescript

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"sync"

	"github.com/rfizzle/astimate/internal/lang/typescript/internal/inspect"
	"github.com/rfizzle/astimate/internal/lang/typescript/internal/resolve"
	"github.com/rfizzle/astimate/internal/metrics"
)

// module is the parse of one module root: every source and test file's
// facts, grouped by package, and the module-wide import graph. It is built
// once by loadModule and read-only afterwards, apart from the details map.
type module struct {
	root string
	// pkgIDs lists the package identifiers, sorted.
	pkgIDs []string
	pkgs   map[string]*pkg
	// testRefs is the module-wide test reference index: every identifier
	// of every package's test files, the TypeScript counterpart of the Go
	// extractor's test reference index. untested_exports reads it, so an
	// export another package's test names counts as tested, and a later
	// metric that needs "referenced from a test anywhere in the module"
	// reads it too.
	testRefs map[string]bool

	detailsMu sync.Mutex
	details   map[string]details
}

// pkg is one package: a directory of the module with at least one
// non-test source file.
type pkg struct {
	id string
	// dir is the absolute directory of the package.
	dir string
	// src and tests are the package's non-test and test files, sorted by
	// path.
	src, tests []*inspect.Facts
	// internal and external and stdlib are the distinct imports of the
	// non-test files by class: internal package identifiers, external
	// package names and Node built-in names.
	internal, external, stdlib map[string]bool
	// reexports counts the names re-exported from other packages.
	reexports int
	// fanIn and fanInTests are the packages importing this one from
	// non-test files, and those importing it from test files only.
	fanIn, fanInTests map[string]bool
}

// details is the per-package record Details serves: the names behind
// untested_exports, those the untested directive left out, and the
// duplicate block locations; the declarations of the untested names, index
// for index, and of the globals, in the order the count reads them, with
// the globals' names index for index; the largest file; and the source files, sorted. Files are relative to the
// package directory in slash form.
type details struct {
	untested, excluded []string
	dupLocations       []string
	untestedPos        []metrics.Position
	globalPos          []metrics.Position
	globalNames        []string
	largestFile        string
	sourceFiles        []string
}

// loadModule finds and parses every TypeScript file of the module at the
// absolute path root, then groups the files into packages and resolves the
// import graph. ctx is checked before each file.
func loadModule(ctx context.Context, root string, opts inspect.Options) (*module, error) {
	files, err := resolve.FindFiles(root)
	if err != nil {
		return nil, fmt.Errorf("listing TypeScript files of %s: %w", root, err)
	}
	m := &module{root: root, pkgs: make(map[string]*pkg), testRefs: make(map[string]bool)}
	for _, f := range files {
		if f.Test {
			continue
		}
		if _, ok := m.pkgs[f.Pkg]; !ok {
			m.pkgs[f.Pkg] = &pkg{
				id:  f.Pkg,
				dir: filepath.Join(root, filepath.FromSlash(f.Pkg)),
			}
			m.pkgIDs = append(m.pkgIDs, f.Pkg)
		}
	}
	slices.Sort(m.pkgIDs)

	cfg, err := resolve.ReadConfig(root)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(m.pkgIDs))
	for _, id := range m.pkgIDs {
		ids[id] = struct{}{}
	}
	res := resolve.New(root, ids, cfg)
	sc := inspect.NewScanner(opts)
	for _, f := range files {
		p, ok := m.pkgs[f.Pkg]
		if !ok {
			// A test file in a directory with no source files belongs to
			// no package.
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("loading %s: %w", root, err)
		}
		ff, err := sc.Scan(f.Abs, f.Rel, f.Test)
		if err != nil {
			return nil, err
		}
		if f.Test {
			p.tests = append(p.tests, ff)
			m.indexTestRefs(ff)
		} else {
			p.src = append(p.src, ff)
		}
	}
	m.link(res)
	return m, nil
}

// indexTestRefs adds the identifiers of f, a test file of a package of m,
// to the module-wide test reference index.
func (m *module) indexTestRefs(f *inspect.Facts) {
	for id := range f.Idents {
		m.testRefs[id] = true
	}
}

// link resolves every file's imports and re-exports into its package's
// import sets and the reverse edges fan_in counts. Imports resolving to the
// importing package itself are intra-package and not counted.
func (m *module) link(res *resolve.Resolver) {
	for _, id := range m.pkgIDs {
		p := m.pkgs[id]
		p.internal, p.external, p.stdlib = map[string]bool{}, map[string]bool{}, map[string]bool{}
		p.fanIn, p.fanInTests = map[string]bool{}, map[string]bool{}
	}
	testOnly := make(map[string]map[string]bool, len(m.pkgIDs))
	for _, id := range m.pkgIDs {
		p := m.pkgs[id]
		for _, f := range p.src {
			for _, spec := range f.Imports {
				imp := res.Classify(f.Dir, spec)
				switch imp.Kind {
				case resolve.Internal:
					if imp.Name != id {
						p.internal[imp.Name] = true
					}
				case resolve.External:
					p.external[imp.Name] = true
				case resolve.Stdlib:
					p.stdlib[imp.Name] = true
				}
			}
			for _, re := range f.Reexports {
				if imp := res.Classify(f.Dir, re.Spec); imp.Kind != resolve.Internal || imp.Name != id {
					p.reexports += re.Names
				}
			}
		}
		for _, f := range p.tests {
			for _, spec := range f.Imports {
				if imp := res.Classify(f.Dir, spec); imp.Kind == resolve.Internal && imp.Name != id {
					if testOnly[imp.Name] == nil {
						testOnly[imp.Name] = map[string]bool{}
					}
					testOnly[imp.Name][id] = true
				}
			}
		}
	}
	for _, id := range m.pkgIDs {
		for target := range m.pkgs[id].internal {
			m.pkgs[target].fanIn[id] = true
		}
	}
	for target, importers := range testOnly {
		t := m.pkgs[target]
		for id := range importers {
			if !t.fanIn[id] {
				t.fanInTests[id] = true
			}
		}
	}
}

// setDetails records d as the most recent details of package id.
func (m *module) setDetails(id string, d details) {
	m.detailsMu.Lock()
	defer m.detailsMu.Unlock()
	if m.details == nil {
		m.details = make(map[string]details)
	}
	m.details[id] = d
}

// detailsOf returns the most recent details recorded for package id, and
// whether any were.
func (m *module) detailsOf(id string) (details, bool) {
	m.detailsMu.Lock()
	defer m.detailsMu.Unlock()
	d, ok := m.details[id]
	return d, ok
}
