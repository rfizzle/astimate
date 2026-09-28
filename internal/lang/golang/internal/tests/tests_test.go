package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// parseTestFile parses src as the file name into fset.
func parseTestFile(tb testing.TB, fset *token.FileSet, name, src string) *ast.File {
	tb.Helper()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		tb.Fatalf("parsing %s: %v", name, err)
	}
	return f
}

func TestTestFuncSignatures(t *testing.T) {
	const header = "package p\n\nimport \"testing\"\n\n"
	for _, tc := range []struct {
		name string
		src  string
		want int
	}{
		{"test", "func TestX(t *testing.T) {}", 1},
		{"bare Test", "func Test(t *testing.T) {}", 1},
		{"test underscore", "func Test_x(t *testing.T) {}", 1},
		{"benchmark", "func BenchmarkX(b *testing.B) {}", 1},
		{"fuzz", "func FuzzX(f *testing.F) {}", 1},
		{"example", "func ExampleX() {}", 1},
		{"bare Example", "func Example() {}", 1},
		{"example suffix", "func ExampleX_second() {}", 1},
		{"extra param", "func TestHelper(t *testing.T, x int) {}", 0},
		{"non-pointer", "func TestX(t testing.T) {}", 0},
		{"lowercase after prefix", "func Testlower(t *testing.T) {}", 0},
		{"method", "type s struct{}\n\nfunc (s) TestM(t *testing.T) {}", 0},
		{"example with param", "func ExampleX(x int) {}", 0},
		{"example with result", "func ExampleX() int { return 0 }", 0},
		{"test with result", "func TestX(t *testing.T) error { return nil }", 0},
		{"benchmark wrong type", "func BenchmarkX(t *testing.T) {}", 0},
		{"fuzz wrong type", "func FuzzX(b *testing.B) {}", 0},
		{"test no params", "func TestX() {}", 0},
		{"generic", "func TestX[P any](t *testing.T) {}", 0},
		{"other package T", "type fake struct{ T int }\n\nfunc TestX(t *fake.T) {}", 0},
		{"lowercase example", "func Examplex() {}", 0},
		{"helper", "func helper(t *testing.T) {}", 0},
		{"examples count once each", "func Example() {}\n\nfunc ExampleX() {}", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := parseTestFile(t, token.NewFileSet(), "p_test.go", header+tc.src)
			if got := countTestFuncs(f); got != tc.want {
				t.Errorf("countTestFuncs = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestTestFuncImportNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want int
	}{
		{"alias", "package p\n\nimport tt \"testing\"\n\nfunc TestX(t *tt.T) {}", 1},
		{"alias used unaliased", "package p\n\nimport tt \"testing\"\n\nfunc TestX(t *testing.T) {}", 0},
		{"dot import", "package p\n\nimport . \"testing\"\n\nfunc TestX(t *T) {}", 1},
		{"not imported", "package p\n\nfunc TestX(t *testing.T) {}\n\nfunc ExampleX() {}", 1},
		{"blank import", "package p\n\nimport _ \"testing\"\n\nfunc TestX(t *testing.T) {}", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := parseTestFile(t, token.NewFileSet(), "p_test.go", tc.src)
			if got := countTestFuncs(f); got != tc.want {
				t.Errorf("countTestFuncs = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestTestMetricsFoldsExternal(t *testing.T) {
	fset := token.NewFileSet()
	src := parseTestFile(t, fset, "/m/p/p.go", "package p\n\nfunc F() {}")
	internal := parseTestFile(t, fset, "/m/p/p_test.go",
		"package p\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) {}")
	external := parseTestFile(t, fset, "/m/p/x_test.go",
		"package p_test\n\nimport \"testing\"\n\nfunc BenchmarkF(b *testing.B) {}")

	const pkg = "example.com/m/p"
	p := &packages.Package{PkgPath: pkg, Syntax: []*ast.File{src}}
	l := &load.Module{
		Fset:   fset,
		Pkgs:   map[string]*packages.Package{pkg: p},
		Tests:  map[string]*packages.Package{pkg: {PkgPath: pkg, Syntax: []*ast.File{src, internal}}},
		XTests: map[string]*packages.Package{pkg: {PkgPath: pkg + "_test", Syntax: []*ast.File{external}}},
	}

	want := Counts{TestFiles: 2, TestFuncs: 2, HasTests: true}
	if got := Count(l, p); got != want {
		t.Errorf("Count = %+v, want %+v", got, want)
	}

	// A file listed by both variants is counted once.
	l.XTests[pkg].Syntax = append(l.XTests[pkg].Syntax, internal)
	if got := Count(l, p); got != want {
		t.Errorf("with a shared file: Count = %+v, want %+v", got, want)
	}

	// Without test variants nothing is counted.
	bare := &load.Module{Fset: fset, Pkgs: l.Pkgs}
	if got := Count(bare, p); got != (Counts{}) {
		t.Errorf("no test variants: Count = %+v, want zero", got)
	}
}

func TestTestMetricsFixture(t *testing.T) {
	l := loadFixture(t)

	t.Run("tested", func(t *testing.T) {
		want := Counts{TestFiles: 3, TestFuncs: 4, HasTests: true}
		if got := Count(l, l.Pkgs["example.com/fixture/tested"]); got != want {
			t.Errorf("Count = %+v, want %+v", got, want)
		}
	})
	t.Run("trivial", func(t *testing.T) {
		if got := Count(l, l.Pkgs["example.com/fixture/trivial"]); got.HasTests {
			t.Errorf("Count = %+v, want has_tests=false", got)
		}
	})
}
