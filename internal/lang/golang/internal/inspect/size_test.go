package inspect

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
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
			if got := FileSLOC(tf, f, []byte(tc.src)); got != tc.want {
				t.Errorf("FileSLOC = %d, want %d", got, tc.want)
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
		want          ExportCounts
		syntacticMiss int
	}{
		{"func", "package p\nfunc F() {}\nfunc f() {}\n", ExportCounts{Symbols: 1}, 0},
		{"type", "package p\ntype T int\ntype t int\ntype (\n\tU int\n\tu int\n)\n", ExportCounts{Symbols: 2, Types: 2}, 0},
		{"var names", "package p\nvar A, b, C = 1, 2, 3\nvar (\n\tD int\n\t_ int\n)\n", ExportCounts{Symbols: 3}, 0},
		{"const names", "package p\nconst (\n\tX = iota\n\tY\n\tz\n)\n", ExportCounts{Symbols: 2}, 0},
		{"method on exported type", "package p\ntype T struct{}\nfunc (T) M() {}\nfunc (*T) m() {}\n", ExportCounts{Symbols: 2, Types: 1}, 0},
		{"method on unexported type", "package p\ntype t struct{}\nfunc (t) M() {}\nfunc (*t) N() {}\n", ExportCounts{Symbols: 2}, 0},
		{"fields and interface methods", "package p\ntype s struct{ F int }\ntype i interface{ M() }\n", ExportCounts{}, 0},
		{"imports", "package p\nimport Fmt \"fmt\"\nvar _ = Fmt.Sprint\n", ExportCounts{}, 0},
		{"only exported type is an interface", "package p\ntype I interface{ M() }\ntype s struct{}\n", ExportCounts{Symbols: 1, Types: 1, InterfaceTypes: 1}, 0},
		{"interface and struct", "package p\ntype (\n\tI interface{ M() }\n\tS struct{}\n)\n", ExportCounts{Symbols: 2, Types: 2, InterfaceTypes: 1}, 0},
		{"generic and constraint interfaces", "package p\ntype G[T any] interface{ Get() T }\ntype N interface{ ~int | ~float64 }\n", ExportCounts{Symbols: 2, Types: 2, InterfaceTypes: 2}, 0},
		{"alias of interface literal", "package p\ntype A = interface{ M() }\n", ExportCounts{Symbols: 1, Types: 1, InterfaceTypes: 1}, 0},
		{"alias of named interface", "package p\nimport \"io\"\ntype R = io.Reader\n", ExportCounts{Symbols: 1, Types: 1, InterfaceTypes: 1}, 1},
		{"definition from named interface", "package p\nimport \"io\"\ntype R io.Reader\n", ExportCounts{Symbols: 1, Types: 1, InterfaceTypes: 1}, 1},
		{"definition from local interface", "package p\ntype I interface{ M() }\ntype J I\n", ExportCounts{Symbols: 2, Types: 2, InterfaceTypes: 2}, 1},
		{"alias of error", "package p\ntype E = error\n", ExportCounts{Symbols: 1, Types: 1, InterfaceTypes: 1}, 1},
		{"definition from named struct", "package p\nimport \"strings\"\ntype B strings.Builder\n", ExportCounts{Symbols: 1, Types: 1}, 0},
		{"alias of struct literal", "package p\ntype S = struct{ X int }\n", ExportCounts{Symbols: 1, Types: 1}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, scope := typeCheckForSize(t, tc.src)
			if got := ExportedSymbols(f, scope); got != tc.want {
				t.Errorf("ExportedSymbols(scope) = %+v, want %+v", got, tc.want)
			}
			syntactic := tc.want
			syntactic.InterfaceTypes -= tc.syntacticMiss
			if got := ExportedSymbols(f, nil); got != syntactic {
				t.Errorf("ExportedSymbols(nil) = %+v, want %+v", got, syntactic)
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
// absolute keys, one per authored non-test file, summing to sloc, while
// files still counts the generated ones.
func TestSizePerFile(t *testing.T) {
	l := loadFixture(t)
	for _, path := range l.Paths {
		t.Run(path, func(t *testing.T) {
			got, err := Size(l, l.Pkgs[path], load.OSFiles{})
			if err != nil {
				t.Fatalf("size: %v", err)
			}
			sum := 0
			for name, n := range got.PerFile {
				if !filepath.IsAbs(name) {
					t.Errorf("PerFile key %q is not absolute", name)
				}
				sum += n
			}
			authored := got.Files - len(l.GeneratedNames(l.Pkgs[path]))
			if len(got.PerFile) != authored || sum != got.SLOC {
				t.Errorf("PerFile has %d files summing to %d, want %d files summing to %d",
					len(got.PerFile), sum, authored, got.SLOC)
			}
		})
	}
}

func BenchmarkSize(b *testing.B) {
	l := loadFixture(b)
	for b.Loop() {
		for _, path := range l.Paths {
			if _, err := Size(l, l.Pkgs[path], load.OSFiles{}); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func TestIsSpace(t *testing.T) {
	for _, b := range []byte(" \t\r\v\f") {
		if !IsSpace(b) {
			t.Errorf("IsSpace(%q) = false, want true", b)
		}
	}
	for _, b := range []byte("\nx/{") {
		if IsSpace(b) {
			t.Errorf("IsSpace(%q) = true, want false", b)
		}
	}
}

// TestSizeCgo checks that a cgo package is measured from the files in
// GoFiles, not the files cgo generated into the build cache: PerFile has
// one entry per GoFiles entry.
func TestSizeCgo(t *testing.T) {
	if !haveCgo(t) {
		t.Skip("cgo or its C compiler is unavailable")
	}
	root := filepath.Join(filepath.Dir(fixtureRoot(t)), "cgo")
	m, err := load.Load(&packages.Config{Dir: root}, packages.Load)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	native := m.Pkgs["example.com/cgo/native"]
	sz, err := Size(m, native, load.OSFiles{})
	if err != nil {
		t.Fatalf("Size(native): %v", err)
	}
	if got := slices.Sorted(maps.Keys(sz.PerFile)); !slices.Equal(got, native.GoFiles) || len(got) != sz.Files {
		t.Errorf("native: PerFile keys = %v, Files = %d, want GoFiles %v", got, sz.Files, native.GoFiles)
	}
}
