package tests

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

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
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
func checkUntestedSrc(t *testing.T, files map[string]string) (*load.Module, *packages.Package) {
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
			Instances:  make(map[*ast.Ident]types.Instance),
			Types:      make(map[ast.Expr]types.TypeAndValue),
		}
		conf := types.Config{Importer: importer.Default()}
		pkg, err := conf.Check(path, fset, fs, info)
		if err != nil {
			t.Fatalf("type-checking: %v", err)
		}
		return &packages.Package{ID: path, PkgPath: path, Syntax: fs, Types: pkg, TypesInfo: info}
	}
	l := &load.Module{
		Fset:   fset,
		Path:   path,
		Pkgs:   make(map[string]*packages.Package),
		Tests:  make(map[string]*packages.Package),
		XTests: make(map[string]*packages.Package),
	}
	p := check(plain)
	l.Pkgs[path] = p
	l.Paths = []string{path}
	if len(all) > len(plain) {
		tp := check(all)
		tp.ID = path + " [" + path + ".test]"
		l.Tests[path] = tp
	}
	return l, p
}

func TestUntestedReferenceTable(t *testing.T) {
	l := loadRoot(t, refsRoot(t))
	got := Untested(l, new(Refs), l.Pkgs["example.com/refs/refs"])
	for _, tc := range []struct {
		how, key string
		tested   bool
	}{
		{"direct call", "Direct", true},
		{"method value", "Counter.Inc", true},
		{"interface dispatch", "Square.Area", true},
		{"generic interface dispatch", "Box.Get", true},
		{"embedded promotion", "Inner.Hello", true},
		{"non-test reference only", "Never", false},
	} {
		t.Run(tc.how, func(t *testing.T) {
			if untested := slices.Contains(got.Names, tc.key); untested == tc.tested {
				t.Errorf("%s untested = %v, want %v (names %v)", tc.key, untested, !tc.tested, got.Names)
			}
		})
	}
	if got.Untested != 1 || !slices.Equal(got.Names, []string{"Never"}) {
		t.Errorf("untested = %d %v, want 1 [Never]", got.Untested, got.Names)
	}
	if want := []string{"Wrapper"}; !slices.Equal(got.Excluded, want) {
		t.Errorf("excluded = %v, want %v", got.Excluded, want)
	}
}

func TestUntestedNoTests(t *testing.T) {
	l := loadRoot(t, refsRoot(t))
	got := Untested(l, new(Refs), l.Pkgs["example.com/refs/notests"])
	if want := []string{"One", "Three", "Two"}; got.Untested != 3 || !slices.Equal(got.Names, want) {
		t.Errorf("untested = %d %v, want 3 %v", got.Untested, got.Names, want)
	}
	if len(got.Excluded) != 0 {
		t.Errorf("excluded = %v, want none", got.Excluded)
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
	got := Untested(l, new(Refs), p)
	if want := []string{"F", "T.M"}; !slices.Equal(got.Excluded, want) {
		t.Errorf("excluded = %v, want %v", got.Excluded, want)
	}
	// The package has no test files, so every exported func and method not
	// under the directive is untested, methods included.
	if want := []string{"G", "T.N"}; got.Untested != 2 || !slices.Equal(got.Names, want) {
		t.Errorf("untested = %d %v, want 2 %v", got.Untested, got.Names, want)
	}
}

// TestUntestedInPackageDispatch covers interface and type-parameter
// dispatch where the interface and the concrete type come from the
// in-package test variant's own type-check, and an interface from another
// package, including generic receivers checked through the instantiations
// the test holds.
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

type Ptr[T any] struct{}

func (*Ptr[T]) Area() int { return 3 }

type Gen[T any] struct{ v T }

func (g Gen[T]) Area() T { return g.v }

type Lone[T any] struct{}

func (Lone[T]) Area() int { return 4 }

type Getter[T any] interface{ Get() T }

type Box[T any] struct{ v T }

func (b Box[T]) Get() T { return b.v }

type StrBox struct{}

func (StrBox) Get() string { return "s" }

type IntBox struct{}

func (IntBox) Get() int { return 1 }

type FloatBox struct{}

func (FloatBox) Get() float64 { return 1 }

type Crate struct{}

func (*Crate) Get() bool { return true }

type Taker[T any] interface{ Take() T }

type Tray[T any] struct{}

func (Tray[T]) Take() T { var v T; return v }

type NestBox struct{}

func (NestBox) Get() uint { return 0 }

type ChainBox struct{}

func (ChainBox) Get() int8 { return 0 }

type SwapBox struct{}

func (SwapBox) Get() int32 { return 0 }

type HarnessBox struct{}

func (HarnessBox) Get() int64 { return 0 }

type WrapBox struct{}

func (WrapBox) Get() uint16 { return 0 }

type IdleBox struct{}

func (IdleBox) Get() uint8 { return 0 }
`,
		"u_test.go": `package u

import (
	"fmt"
	"testing"
)

func area[S Shape](s S) int { return s.Area() }

func get[T any](g Getter[T]) T { return g.Get() }

func via[T any, G Getter[T]](g G) T { return g.Get() }

func take[T any](k Taker[T]) T { return k.Take() }

func outer[T any](g Getter[T]) T { return get(g) }

func chain1[T any](g Getter[T]) T { return chain2(g) }

func chain2[T any](g Getter[T]) T { return get[T](g) }

func rec[T any](n int, g Getter[T]) T {
	if n > 0 {
		return rec(n-1, g)
	}
	return g.Get()
}

func swap[A, B any](a Getter[A], b Getter[B]) A {
	if a == nil {
		_ = swap(b, a)
	}
	return a.Get()
}

type harness[T any] struct{ g Getter[T] }

func (h harness[T]) run() T { return h.g.Get() }

func wrap[T any](g Getter[T]) T { return harness[T]{g: g}.run() }

type idle[T any] struct{ g Getter[T] }

func (i *idle[T]) run() T { return i.g.Get() }

func TestDispatch(t *testing.T) {
	var s Shape = &Circle{}
	_ = s.Area()
	_ = area(Sq{})
	var st fmt.Stringer = Name{}
	_ = st.String()
	_ = area(&Ptr[int]{})
	_ = Gen[string]{}
	_ = get[string](Box[string]{})
	_ = get(IntBox{})
	_ = via[bool](&Crate{})
	_ = Tray[string]{}
	_ = outer(NestBox{})
	_ = chain1(ChainBox{})
	_ = rec(1, IntBox{})
	_ = swap[int16, int32](nil, SwapBox{})
	_ = harness[int64]{g: HarnessBox{}}.run()
	_ = wrap(WrapBox{})
}
`,
	})
	got := Untested(l, new(Refs), p)
	// Blob.Area has the wrong signature for Shape; Other.Len is never used.
	// Gen[string].Area returns a string, so the only instantiation the test
	// holds does not implement Shape. Lone is never instantiated, so no
	// value of it can reach Shape.Area even though its method matches.
	// get's instantiations are [string] and [int]: Box[string] and StrBox
	// implement Getter[string] and IntBox implements Getter[int], but
	// FloatBox implements no Getter the tests instantiate. The int
	// instantiation is inferred from the argument. via selects Get
	// on its type argument *Crate directly. take is never called, so
	// Tray.Take stays untested although the test holds Tray[string].
	// NestBox.Get is reached only through outer[uint] calling get[uint], and
	// ChainBox.Get through chain1, chain2 and get. rec calls itself with its
	// own type argument and swap with its arguments swapped, so resolving
	// them must stop; swap[int16, int32] reaches SwapBox.Get only through
	// swap[int32, int16]. HarnessBox.Get is reached only through the method
	// of harness[int64], and WrapBox.Get through harness[uint16]
	// instantiated inside wrap. idle is never instantiated, so IdleBox.Get
	// stays untested although idle's method selects Get.
	want := []string{"Blob.Area", "FloatBox.Get", "Gen.Area", "IdleBox.Get", "Lone.Area", "Other.Len", "Tray.Take"}
	if got.Untested != len(want) || !slices.Equal(got.Names, want) {
		t.Errorf("untested = %d %v, want %d %v", got.Untested, got.Names, len(want), want)
	}
}

func BenchmarkUntestedExports(b *testing.B) {
	l := loadFixture(b)
	b.ReportAllocs()
	for b.Loop() {
		// One index per load, as the extractor holds it.
		var refs Refs
		for _, path := range l.Paths {
			Untested(l, &refs, l.Pkgs[path])
		}
	}
}
