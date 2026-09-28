package imports

import (
	"go/ast"
	"slices"
	"strings"
	"sync"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// FanIn holds the fan-in metrics fan_in and fan_in_tests of one package.
type FanIn struct {
	FanIn, FanInTests int
}

// Graph holds a module's reverse import graphs, built once on first use.
// The zero value is ready to use; one Graph belongs to one load.Module and
// is shared, like the Module, by every package measured in it.
type Graph struct {
	once sync.Once
	// reverse maps an import path to the sorted import paths of the module
	// packages that import it from non-test files.
	reverse map[string][]string
	// tests maps an import path to the sorted import paths of the module
	// packages that import it from test files only.
	tests map[string][]string
	// builds counts completed builds; it is 1 after the first use.
	builds int
}

// FanIn returns the number of distinct module packages of m that import p
// from non-test files, and the number that import it from test files only.
// Main packages count as importers like any other. A package's own external
// test package importing it is not an edge (SPEC.md section 6.5). An edge is
// what Imported lists, classified internal by Classify, exactly the edges
// internal_imports counts from the other end.
func (g *Graph) FanIn(m *load.Module, p *packages.Package) FanIn {
	g.build(m)
	return FanIn{
		FanIn:      len(g.reverse[p.PkgPath]),
		FanInTests: len(g.tests[p.PkgPath]),
	}
}

// Importers returns the sorted import paths of the module packages of m
// whose non-test files import the package at importPath, the edges fan_in
// counts. The slice is shared; callers clone it before handing it out.
func (g *Graph) Importers(m *load.Module, importPath string) []string {
	g.build(m)
	return g.reverse[importPath]
}

// build fills the graphs from every module package of m on its first call
// and does nothing afterwards, so the graphs are built once per load however
// many packages are measured. The non-test edges come from the source files
// of each package (load.Module.SourceSyntax), and the test-only edges from
// the _test.go files of its test variants, so neither sees an import that
// only cgo-generated files hold.
func (g *Graph) build(m *load.Module) {
	g.once.Do(func() {
		reverse := make(map[string][]string, len(m.Pkgs))
		tests := make(map[string][]string)
		for from, q := range m.Pkgs {
			direct := make(map[string]bool, len(q.Imports))
			for _, imp := range Imported(q, m.SourceSyntax(q)) {
				direct[imp.PkgPath] = true
				if Classify(m, imp) == Internal {
					reverse[imp.PkgPath] = append(reverse[imp.PkgPath], from)
				}
			}
			for _, tp := range m.TestPackages(q) {
				for _, imp := range Imported(tp, testFiles(m, tp)) {
					path := imp.PkgPath
					if path == from || direct[path] || Classify(m, imp) != Internal {
						continue
					}
					tests[path] = append(tests[path], from)
				}
			}
		}
		sortDedupe(reverse)
		sortDedupe(tests)
		g.reverse = reverse
		g.tests = tests
		g.builds++
	})
}

// testFiles returns the syntax trees of the _test.go files of tp, a test
// variant, leaving out the non-test files an in-package test variant also
// holds.
func testFiles(m *load.Module, tp *packages.Package) []*ast.File {
	out := make([]*ast.File, 0, len(tp.Syntax))
	for _, f := range tp.Syntax {
		if strings.HasSuffix(m.Fset.Position(f.Package).Filename, "_test.go") {
			out = append(out, f)
		}
	}
	return out
}

// sortDedupe sorts each importer list of g and removes repeats, which arise
// when both test variants of one package import the same path.
func sortDedupe(g map[string][]string) {
	for path, from := range g {
		slices.Sort(from)
		g[path] = slices.Compact(from)
	}
}
