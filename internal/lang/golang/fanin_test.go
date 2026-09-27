package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// synth builds packages with parsed source for the fan-in tests, all in one
// file set.
type synth struct {
	t    *testing.T
	fset *token.FileSet
}

func newSynth(t *testing.T) *synth {
	return &synth{t: t, fset: token.NewFileSet()}
}

// pkg returns a package at path with one non-test file importing imports;
// see file for the import syntax.
func (s *synth) pkg(path string, imports ...string) *packages.Package {
	return s.file(path, "p.go", imports)
}

// test returns a test variant or external test package at path with one
// _test.go file importing imports.
func (s *synth) test(path string, imports ...string) *packages.Package {
	return s.file(path, "p_test.go", imports)
}

// file returns a package at path whose one file, named name, imports
// imports and whose Imports resolve them. An import is written as the spec
// would be, without quotes: "b", "_ b" or ". b". A path written "i=>p" is
// imported as i and resolves to a package whose PkgPath is p, as a
// vendored package does.
func (s *synth) file(path, name string, imports []string) *packages.Package {
	s.t.Helper()
	p := &packages.Package{ID: path, PkgPath: path, Imports: make(map[string]*packages.Package, len(imports))}
	var src strings.Builder
	src.WriteString("package p\n")
	for _, imp := range imports {
		spec, pkgPath := imp, ""
		if i := strings.Index(imp, "=>"); i >= 0 {
			spec, pkgPath = imp[:i], imp[i+2:]
		}
		importName, importPath, named := strings.Cut(spec, " ")
		if !named {
			importName, importPath = "", spec
		}
		if pkgPath == "" {
			pkgPath = importPath
		}
		src.WriteString("import " + importName + " " + strconv.Quote(importPath) + "\n")
		p.Imports[importPath] = &packages.Package{ID: pkgPath, PkgPath: pkgPath}
	}
	f, err := parser.ParseFile(s.fset, filepath.Join("/src", path, name), src.String(), parser.ImportsOnly)
	if err != nil {
		s.t.Fatalf("parsing %s: %v", path, err)
	}
	p.Syntax = []*ast.File{f}
	return p
}

// load returns a load of module example.com/m, in s's file set, holding
// pkgs as its non-test packages.
func (s *synth) load(pkgs ...*packages.Package) *loaded {
	l := &loaded{
		fset:       s.fset,
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
	s := newSynth(t)
	l := s.load(
		s.pkg(c, b, "strings", "example.org/ext"),
		s.pkg(a, b),
		s.pkg(b),
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

// TestFanInMatchesFanOut checks that fan_in and internal_imports count the
// same edges under each import rule: blank and dot imports count on both
// sides, an import only a generated file holds counts on neither, and an
// import whose path differs from its package path counts on both under the
// package path.
func TestFanInMatchesFanOut(t *testing.T) {
	const (
		a = "example.com/m/a"
		b = "example.com/m/b"
		v = "example.com/m/vendor/x/v"
	)
	for _, tc := range []struct {
		name    string
		imports []string
		// generated is an internal import in Imports that no source file
		// holds, as cgo adds runtime/cgo to the files it generates.
		generated string
		// target is the package the edge must reach.
		target string
		want   int
	}{
		{"plain", []string{b}, "", b, 1},
		{"blank", []string{"_ " + b}, "", b, 1},
		{"dot", []string{". " + b}, "", b, 1},
		{"plain and blank", []string{b, "_ " + b}, "", b, 1},
		{"generated only", nil, b, b, 0},
		{"import path differs from package path", []string{"x/v=>" + v}, "", v, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSynth(t)
			from := s.pkg(a, tc.imports...)
			if tc.generated != "" {
				from.Imports[tc.generated] = &packages.Package{ID: tc.generated, PkgPath: tc.generated}
			}
			l := s.load(from, s.pkg(b), s.pkg(v))
			out := imports(l, l.pkgs[a]).internal
			in := fanIn(l, l.pkgs[tc.target]).fanIn
			if out != tc.want || in != tc.want {
				t.Errorf("internal_imports(a) = %d, fan_in(%s) = %d, want %d on both sides", out, tc.target, in, tc.want)
			}
		})
	}
}

func TestReverseGraphTestOnly(t *testing.T) {
	const (
		a   = "example.com/m/a"
		b   = "example.com/m/b"
		c   = "example.com/m/c"
		cmd = "example.com/m/cmd/tool"
	)
	s := newSynth(t)
	// cmd is a main package importing a; it counts as an importer.
	l := s.load(s.pkg(a, b), s.pkg(b), s.pkg(c), s.pkg(cmd, a))
	// c imports b only from its in-package test and external test files;
	// the external test also imports c itself, which is not an edge.
	l.tests[c] = s.test(c, b, "testing")
	l.xtests[c] = s.test(c+"_test", c, "_ "+b)
	// a's test variant repeats its non-test import of b, which must not
	// make a a test-only importer.
	l.tests[a] = s.test(a, b)
	// b's external test imports b; its own package never counts.
	l.xtests[b] = s.test(b+"_test", b)

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
	only := s.load(s.pkg(b), s.pkg(c))
	only.tests[c] = s.test(c, b)
	if got, want := fanIn(only, only.pkgs[b]), (fanInCounts{fanInTests: 1}); got != want {
		t.Errorf("test-only fanIn(b) = %+v, want %+v", got, want)
	}

	// An in-package test variant also holds the non-test files; an import
	// only a non-test file of the variant holds is not a test edge.
	variant := s.load(s.pkg(b), s.pkg(c))
	tp := s.test(c)
	tp.Syntax = append(tp.Syntax, s.pkg(c, b).Syntax...)
	tp.Imports[b] = &packages.Package{ID: b, PkgPath: b}
	variant.tests[c] = tp
	if got := fanIn(variant, variant.pkgs[b]); got != (fanInCounts{}) {
		t.Errorf("fanIn(b) with only a non-test file importing it in c's test variant = %+v, want none", got)
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
