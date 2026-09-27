package golang

import (
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"
)

// synthPkg returns a package at path whose Imports hold the given paths.
func synthPkg(path string, imports ...string) *packages.Package {
	p := &packages.Package{ID: path, PkgPath: path, Imports: make(map[string]*packages.Package, len(imports))}
	for _, imp := range imports {
		p.Imports[imp] = &packages.Package{ID: imp, PkgPath: imp}
	}
	return p
}

// synthLoad returns a load of module example.com/m holding pkgs as its
// non-test packages.
func synthLoad(pkgs ...*packages.Package) *loaded {
	l := &loaded{
		modulePath: "example.com/m",
		pkgs:       make(map[string]*packages.Package, len(pkgs)),
		tests:      make(map[string]*packages.Package),
		xtests:     make(map[string]*packages.Package),
	}
	for _, p := range pkgs {
		l.pkgs[p.PkgPath] = p
		l.paths = append(l.paths, p.PkgPath)
	}
	slices.Sort(l.paths)
	return l
}

func TestReverseGraphSynthetic(t *testing.T) {
	const (
		a = "example.com/m/a"
		b = "example.com/m/b"
		c = "example.com/m/c"
	)
	l := synthLoad(
		synthPkg(c, b, "strings", "example.org/ext"),
		synthPkg(a, b),
		synthPkg(b),
	)
	buildReverse(l)
	if got, want := l.reverse[b], []string{a, c}; !slices.Equal(got, want) {
		t.Errorf("reverse[b] = %v, want %v", got, want)
	}
	for _, p := range []string{a, c, "strings", "example.org/ext"} {
		if got := l.reverse[p]; got != nil {
			t.Errorf("reverse[%s] = %v, want none", p, got)
		}
	}
	for _, tc := range []struct {
		path string
		want fanInCounts
	}{
		{a, fanInCounts{}},
		{b, fanInCounts{fanIn: 2}},
		{c, fanInCounts{}},
	} {
		if got := fanIn(l, l.pkgs[tc.path]); got != tc.want {
			t.Errorf("fanIn(%s) = %+v, want %+v", tc.path, got, tc.want)
		}
	}
}

func TestReverseGraphTestOnly(t *testing.T) {
	const (
		a   = "example.com/m/a"
		b   = "example.com/m/b"
		c   = "example.com/m/c"
		cmd = "example.com/m/cmd/tool"
	)
	// cmd is a main package importing a; it counts as an importer.
	l := synthLoad(synthPkg(a, b), synthPkg(b), synthPkg(c), synthPkg(cmd, a))
	// c imports b only from its in-package test and external test files;
	// the external test also imports c itself, which is not an edge.
	l.tests[c] = synthPkg(c, b, "testing")
	l.xtests[c] = synthPkg(c+"_test", c, b)
	// a's test variant repeats its non-test import of b, which must not
	// make a a test-only importer.
	l.tests[a] = synthPkg(a, b)
	// b's external test imports b; its own package never counts.
	l.xtests[b] = synthPkg(b+"_test", b)

	for _, tc := range []struct {
		path string
		want fanInCounts
	}{
		{a, fanInCounts{fanIn: 1}},
		{b, fanInCounts{fanIn: 1, fanInTests: 1}},
		{c, fanInCounts{}},
		{cmd, fanInCounts{}},
	} {
		if got := fanIn(l, l.pkgs[tc.path]); got != tc.want {
			t.Errorf("fanIn(%s) = %+v, want %+v", tc.path, got, tc.want)
		}
	}
	if got, want := l.fanIn.tests[b], []string{c}; !slices.Equal(got, want) {
		t.Errorf("test-only importers of b = %v, want %v", got, want)
	}
	if got, want := l.reverse[a], []string{cmd}; !slices.Equal(got, want) {
		t.Errorf("importers of a = %v, want %v", got, want)
	}

	// c imports b only from tests: fan_in=0, fan_in_tests=1 for a package
	// no non-test file imports.
	only := synthLoad(synthPkg(b), synthPkg(c))
	only.tests[c] = synthPkg(c, b)
	if got, want := fanIn(only, only.pkgs[b]), (fanInCounts{fanInTests: 1}); got != want {
		t.Errorf("test-only fanIn(b) = %+v, want %+v", got, want)
	}
}

func TestFanInFixture(t *testing.T) {
	l := loadFixture(t)

	t.Run("hub", func(t *testing.T) {
		if got := fanIn(l, l.pkgs["example.com/fixture/hub"]); got.fanIn != 4 {
			t.Errorf("fanIn = %+v, want fan_in=4", got)
		}
	})
	t.Run("trivial", func(t *testing.T) {
		if got := fanIn(l, l.pkgs["example.com/fixture/trivial"]); got.fanIn != 0 {
			t.Errorf("fanIn = %+v, want fan_in=0", got)
		}
	})

	// Every package's counts come from the one graph build.
	for _, pkg := range l.paths {
		fanIn(l, l.pkgs[pkg])
	}

	if n := len(l.paths); n != 7 {
		t.Errorf("fixture has %d packages, want 7", n)
	}
	if l.fanIn.builds != 1 {
		t.Errorf("reverse graph built %d times, want 1", l.fanIn.builds)
	}
}

func BenchmarkReverseGraph(b *testing.B) {
	fixture := fixtureRoot(b)
	for _, bc := range []struct{ name, dir string }{
		{"fixture", fixture},
		// fixture is <repo>/testdata/go/fixture.
		{"self", filepath.Dir(filepath.Dir(filepath.Dir(fixture)))},
	} {
		b.Run(bc.name, func(b *testing.B) {
			src, err := loadModule(&packages.Config{Dir: bc.dir}, packages.Load)
			if err != nil {
				b.Fatalf("loading %s: %v", bc.dir, err)
			}
			b.ReportAllocs()
			for b.Loop() {
				l := &loaded{modulePath: src.modulePath, pkgs: src.pkgs, tests: src.tests, xtests: src.xtests}
				buildReverse(l)
			}
		})
	}
}
