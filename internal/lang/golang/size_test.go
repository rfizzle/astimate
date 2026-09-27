package golang

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"testing"
)

// parseForSize parses src as one file into a fresh file set.
func parseForSize(t *testing.T, src string) (*token.File, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	return fset.File(f.FileStart), f
}

func TestFileSLOC(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"package only", "package p\n", 1},
		{"no trailing newline", "package p", 1},
		{"package and comments", "// Package p does things.\n\n// More.\npackage p // trailing\n\n/* block\n   comment */\n// end\n", 1},
		{"blank lines", "package p\n\n\t \n\nvar x = 1\n\n", 2},
		{"line comments", "package p\n// one\n  // two\nvar x = 1\n", 2},
		{"block comment lines", "package p\n/*\nvar y = 2\n*/\nvar x = 1\n", 2},
		{"trailing line comment", "package p\nvar x = 1 // note\n", 2},
		{"trailing block comment", "package p\nvar x = 1 /* note */\n", 2},
		{"leading block comment", "package p\n/* note */ var x = 1\n", 2},
		{"code after block end", "package p\n/* a\nb */ var x = 1\n", 2},
		{"comment marker in string", "package p\nvar s = \"// not a comment\"\n", 2},
		{"multiline raw string", "package p\nvar s = `\n\n// kept\n`\n", 4},
		{"crlf", "package p\r\n\r\n// c\r\nvar x = 1\r\n", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tf, f := parseForSize(t, tc.src)
			if got := fileSLOC(tf, f, []byte(tc.src)); got != tc.want {
				t.Errorf("fileSLOC = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestExportedSymbols(t *testing.T) {
	// want is the count with the package scope. syntacticMiss is how many of
	// want.interfaceTypes the syntactic fallback (nil scope) does not see
	// because the type names an interface instead of spelling one out.
	cases := []struct {
		name          string
		src           string
		want          exportCounts
		syntacticMiss int
	}{
		{"func", "package p\nfunc F() {}\nfunc f() {}\n", exportCounts{symbols: 1}, 0},
		{"type", "package p\ntype T int\ntype t int\ntype (\n\tU int\n\tu int\n)\n", exportCounts{symbols: 2, types: 2}, 0},
		{"var names", "package p\nvar A, b, C = 1, 2, 3\nvar (\n\tD int\n\t_ int\n)\n", exportCounts{symbols: 3}, 0},
		{"const names", "package p\nconst (\n\tX = iota\n\tY\n\tz\n)\n", exportCounts{symbols: 2}, 0},
		{"method on exported type", "package p\ntype T struct{}\nfunc (T) M() {}\nfunc (*T) m() {}\n", exportCounts{symbols: 2, types: 1}, 0},
		{"method on unexported type", "package p\ntype t struct{}\nfunc (t) M() {}\nfunc (*t) N() {}\n", exportCounts{symbols: 2}, 0},
		{"fields and interface methods", "package p\ntype s struct{ F int }\ntype i interface{ M() }\n", exportCounts{}, 0},
		{"imports", "package p\nimport Fmt \"fmt\"\nvar _ = Fmt.Sprint\n", exportCounts{}, 0},
		{"only exported type is an interface", "package p\ntype I interface{ M() }\ntype s struct{}\n", exportCounts{symbols: 1, types: 1, interfaceTypes: 1}, 0},
		{"interface and struct", "package p\ntype (\n\tI interface{ M() }\n\tS struct{}\n)\n", exportCounts{symbols: 2, types: 2, interfaceTypes: 1}, 0},
		{"generic and constraint interfaces", "package p\ntype G[T any] interface{ Get() T }\ntype N interface{ ~int | ~float64 }\n", exportCounts{symbols: 2, types: 2, interfaceTypes: 2}, 0},
		{"alias of interface literal", "package p\ntype A = interface{ M() }\n", exportCounts{symbols: 1, types: 1, interfaceTypes: 1}, 0},
		{"alias of named interface", "package p\nimport \"io\"\ntype R = io.Reader\n", exportCounts{symbols: 1, types: 1, interfaceTypes: 1}, 1},
		{"definition from named interface", "package p\nimport \"io\"\ntype R io.Reader\n", exportCounts{symbols: 1, types: 1, interfaceTypes: 1}, 1},
		{"definition from local interface", "package p\ntype I interface{ M() }\ntype J I\n", exportCounts{symbols: 2, types: 2, interfaceTypes: 2}, 1},
		{"alias of error", "package p\ntype E = error\n", exportCounts{symbols: 1, types: 1, interfaceTypes: 1}, 1},
		{"definition from named struct", "package p\nimport \"strings\"\ntype B strings.Builder\n", exportCounts{symbols: 1, types: 1}, 0},
		{"alias of struct literal", "package p\ntype S = struct{ X int }\n", exportCounts{symbols: 1, types: 1}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, scope := typeCheckForSize(t, tc.src)
			if got := exportedSymbols(f, scope); got != tc.want {
				t.Errorf("exportedSymbols(scope) = %+v, want %+v", got, tc.want)
			}
			syntactic := tc.want
			syntactic.interfaceTypes -= tc.syntacticMiss
			if got := exportedSymbols(f, nil); got != syntactic {
				t.Errorf("exportedSymbols(nil) = %+v, want %+v", got, syntactic)
			}
		})
	}
}

// typeCheckForSize parses src as one file and type-checks it, importing
// standard-library packages from source, and returns the file and the
// package scope.
func typeCheckForSize(t *testing.T, src string) (*ast.File, *types.Scope) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	pkg, err := conf.Check("p", fset, []*ast.File{f}, nil)
	if err != nil {
		t.Fatalf("type-checking: %v", err)
	}
	return f, pkg.Scope()
}

// TestSizePerFile checks the per-file SLOC map duplication weighs lines by:
// absolute keys, one per non-test file, summing to sloc.
func TestSizePerFile(t *testing.T) {
	l := loadFixture(t)
	for _, path := range l.paths {
		t.Run(path, func(t *testing.T) {
			got, err := size(l, l.pkgs[path], osFiles{})
			if err != nil {
				t.Fatalf("size: %v", err)
			}
			sum := 0
			for name, n := range got.perFile {
				if !filepath.IsAbs(name) {
					t.Errorf("perFile key %q is not absolute", name)
				}
				sum += n
			}
			if len(got.perFile) != got.files || sum != got.sloc {
				t.Errorf("perFile has %d files summing to %d, want %d files summing to %d",
					len(got.perFile), sum, got.files, got.sloc)
			}
		})
	}
}

func BenchmarkSize(b *testing.B) {
	l := loadFixture(b)
	for b.Loop() {
		for _, path := range l.paths {
			if _, err := size(l, l.pkgs[path], osFiles{}); err != nil {
				b.Fatal(err)
			}
		}
	}
}
