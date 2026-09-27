package golang

import (
	"slices"
	"sync"

	"golang.org/x/tools/go/packages"
)

// fanInCounts holds the fan-in metrics fan_in and fan_in_tests of one
// package.
type fanInCounts struct {
	fanIn, fanInTests int
}

// reverseGraph guards the one-time build of the module's reverse import
// graphs and holds the test-only one. The non-test graph lives in
// loaded.reverse.
type reverseGraph struct {
	once sync.Once
	// tests maps an import path to the sorted import paths of the module
	// packages that import it from test files only.
	tests map[string][]string
	// builds counts completed builds; it is 1 after the first fan-in call.
	builds int
}

// fanIn returns the number of distinct module packages that import p from
// non-test files, and the number that import it from test files only. Main
// packages count as importers like any other. A package's own external test
// package importing it is not an edge (SPEC.md section 6.5).
func fanIn(l *loaded, p *packages.Package) fanInCounts {
	buildReverse(l)
	return fanInCounts{
		fanIn:      len(l.reverse[p.PkgPath]),
		fanInTests: len(l.fanIn.tests[p.PkgPath]),
	}
}

// buildReverse fills l.reverse and l.fanIn.tests from every module package
// on its first call for l and does nothing afterwards, so the graphs are
// built once per load however many packages are measured.
func buildReverse(l *loaded) {
	l.fanIn.once.Do(func() {
		reverse := make(map[string][]string, len(l.pkgs))
		tests := make(map[string][]string)
		for from, q := range l.pkgs {
			for path := range q.Imports {
				if isInternal(l, path) {
					reverse[path] = append(reverse[path], from)
				}
			}
			for _, tp := range testPackagesFor(l, q) {
				for path := range tp.Imports {
					if path == from || !isInternal(l, path) {
						continue
					}
					if _, direct := q.Imports[path]; !direct {
						tests[path] = append(tests[path], from)
					}
				}
			}
		}
		sortDedupe(reverse)
		sortDedupe(tests)
		l.reverse = reverse
		l.fanIn.tests = tests
		l.fanIn.builds++
	})
}

// sortDedupe sorts each importer list of g and removes repeats, which arise
// when both test variants of one package import the same path.
func sortDedupe(g map[string][]string) {
	for path, from := range g {
		slices.Sort(from)
		g[path] = slices.Compact(from)
	}
}
