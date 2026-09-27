package golang

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// refsRoot returns the absolute path of testdata/go/refs.
func refsRoot(tb testing.TB) string {
	tb.Helper()
	return filepath.Join(filepath.Dir(fixtureRoot(tb)), "refs")
}

// checkUntestedSrc type-checks files, a map from file name to source, as
// the package example.com/u, and returns a load holding it: the non-test
// files as the package and, when a _test.go file is present, all files
// together as its in-package test variant.
func checkUntestedSrc(t *testing.T, files map[string]string) (*loaded, *packages.Package) {
	t.Helper()
	const path = "example.com/u"
	fset := token.NewFileSet()
	var plain, all []*ast.File
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, files[name], parser.ParseComments)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		all = append(all, f)
		if !strings.HasSuffix(name, "_test.go") {
			plain = append(plain, f)
		}
	}
	check := func(fs []*ast.File) *packages.Package {
		info := &types.Info{
			Defs:       make(map[*ast.Ident]types.Object),
			Uses:       make(map[*ast.Ident]types.Object),
			Selections: make(map[*ast.SelectorExpr]*types.Selection),
		}
		conf := types.Config{Importer: importer.Default()}
		pkg, err := conf.Check(path, fset, fs, info)
		if err != nil {
			t.Fatalf("type-checking: %v", err)
		}
		return &packages.Package{ID: path, PkgPath: path, Syntax: fs, Types: pkg, TypesInfo: info}
	}
	l := &loaded{
		fset:       fset,
		modulePath: path,
		pkgs:       make(map[string]*packages.Package),
		tests:      make(map[string]*packages.Package),
		xtests:     make(map[string]*packages.Package),
	}
	p := check(plain)
	l.pkgs[path] = p
	l.paths = []string{path}
	if len(all) > len(plain) {
		tp := check(all)
		tp.ID = path + " [" + path + ".test]"
		l.tests[path] = tp
	}
	return l, p
}

func TestUntestedReferenceTable(t *testing.T) {
	l := loadRoot(t, refsRoot(t))
	got := untestedExports(l, l.pkgs["example.com/refs/refs"])
	for _, tc := range []struct {
		how, key string
		tested   bool
	}{
		{"direct call", "Direct", true},
		{"method value", "Counter.Inc", true},
		{"interface dispatch", "Square.Area", true},
		{"embedded promotion", "Inner.Hello", true},
		{"non-test reference only", "Never", false},
	} {
		t.Run(tc.how, func(t *testing.T) {
			if untested := slices.Contains(got.names, tc.key); untested == tc.tested {
				t.Errorf("%s untested = %v, want %v (names %v)", tc.key, untested, !tc.tested, got.names)
			}
		})
	}
	if got.untested != 1 || !slices.Equal(got.names, []string{"Never"}) {
		t.Errorf("untested = %d %v, want 1 [Never]", got.untested, got.names)
	}
	if want := []string{"Wrapper"}; !slices.Equal(got.excluded, want) {
		t.Errorf("excluded = %v, want %v", got.excluded, want)
	}
}

func TestUntestedNoTests(t *testing.T) {
	l := loadRoot(t, refsRoot(t))
	got := untestedExports(l, l.pkgs["example.com/refs/notests"])
	if want := []string{"One", "Three", "Two"}; got.untested != 3 || !slices.Equal(got.names, want) {
		t.Errorf("untested = %d %v, want 3 %v", got.untested, got.names, want)
	}
	if len(got.excluded) != 0 {
		t.Errorf("excluded = %v, want none", got.excluded)
	}
}

func TestUntestedDirective(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  []string
		want bool
	}{
		{"none", nil, false},
		{"bare", []string{"//astimate:untested"}, true},
		{"with reason", []string{"// F wraps G.", "//", "//astimate:untested wraps G"}, true},
		{"longer word", []string{"//astimate:untestedness"}, false},
		{"spaced comment", []string{"// astimate:untested"}, false},
		{"block comment", []string{"/*astimate:untested*/"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var doc *ast.CommentGroup
			if tc.doc != nil {
				doc = &ast.CommentGroup{}
				for _, text := range tc.doc {
					doc.List = append(doc.List, &ast.Comment{Text: text})
				}
			}
			if got := hasUntestedDirective(doc); got != tc.want {
				t.Errorf("hasUntestedDirective(%q) = %v, want %v", tc.doc, got, tc.want)
			}
		})
	}

	l, p := checkUntestedSrc(t, map[string]string{"u.go": `package u

// F is excluded.
//
//astimate:untested kept for callers
func F() {}

//astimate:untested
func (T) M() {}

// astimate:untested is not the directive.
func G() {}

type T struct{}

func (*T) N() {}

//astimate:untested
func unexported() {}
`})
	got := untestedExports(l, p)
	if want := []string{"F", "T.M"}; !slices.Equal(got.excluded, want) {
		t.Errorf("excluded = %v, want %v", got.excluded, want)
	}
	// The package has no test files, so every exported func and method not
	// under the directive is untested, methods included.
	if want := []string{"G", "T.N"}; got.untested != 2 || !slices.Equal(got.names, want) {
		t.Errorf("untested = %d %v, want 2 %v", got.untested, got.names, want)
	}
}

// TestUntestedInPackageDispatch covers interface and type-parameter
// dispatch where the interface and the concrete type come from the
// in-package test variant's own type-check, and an interface from another
// package.
func TestUntestedInPackageDispatch(t *testing.T) {
	l, p := checkUntestedSrc(t, map[string]string{
		"u.go": `package u

type Shape interface{ Area() int }

type Sq struct{}

func (Sq) Area() int { return 1 }

type Circle struct{}

func (*Circle) Area() int { return 2 }

type Blob struct{}

func (Blob) Area(scale int) int { return scale }

type Name struct{}

func (Name) String() string { return "n" }

type Other struct{}

func (Other) Len() int { return 0 }
`,
		"u_test.go": `package u

import (
	"fmt"
	"testing"
)

func area[S Shape](s S) int { return s.Area() }

func TestDispatch(t *testing.T) {
	var s Shape = &Circle{}
	_ = s.Area()
	_ = area(Sq{})
	var st fmt.Stringer = Name{}
	_ = st.String()
}
`,
	})
	got := untestedExports(l, p)
	// Blob.Area has the wrong signature for Shape; Other.Len is never used.
	if want := []string{"Blob.Area", "Other.Len"}; got.untested != 2 || !slices.Equal(got.names, want) {
		t.Errorf("untested = %d %v, want 2 %v", got.untested, got.names, want)
	}
}

func BenchmarkUntestedExports(b *testing.B) {
	l := loadFixture(b)
	b.ReportAllocs()
	for b.Loop() {
		for _, path := range l.paths {
			untestedExports(l, l.pkgs[path])
		}
	}
}
