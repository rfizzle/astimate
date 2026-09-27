package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics/metricstest"
	"golang.org/x/tools/go/packages"
)

// loadForSize loads the fixture module once for a size test.
func loadForSize(tb testing.TB) *loaded {
	tb.Helper()
	l, err := loadModule(&packages.Config{Dir: fixtureRoot(tb)}, packages.Load)
	if err != nil {
		tb.Fatalf("loading fixture: %v", err)
	}
	return l
}

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
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"func", "package p\nfunc F() {}\nfunc f() {}\n", 1},
		{"type", "package p\ntype T int\ntype t int\ntype (\n\tU int\n\tu int\n)\n", 2},
		{"var names", "package p\nvar A, b, C = 1, 2, 3\nvar (\n\tD int\n\t_ int\n)\n", 3},
		{"const names", "package p\nconst (\n\tX = iota\n\tY\n\tz\n)\n", 2},
		{"method on exported type", "package p\ntype T struct{}\nfunc (T) M() {}\nfunc (*T) m() {}\n", 2},
		{"method on unexported type", "package p\ntype t struct{}\nfunc (t) M() {}\nfunc (*t) N() {}\n", 2},
		{"fields and interface methods", "package p\ntype s struct{ F int }\ntype i interface{ M() }\n", 0},
		{"imports", "package p\nimport Fmt \"fmt\"\nvar _ = Fmt.Sprint\n", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, f := parseForSize(t, tc.src)
			if got := exportedSymbols(f); got != tc.want {
				t.Errorf("exportedSymbols = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSizeGoldens(t *testing.T) {
	l := loadForSize(t)
	goldenDir := filepath.Join(fixtureRoot(t), "golden")
	for _, path := range l.paths {
		t.Run(path, func(t *testing.T) {
			p := l.pkgs[path]
			got, err := size(l, p)
			if err != nil {
				t.Fatalf("size: %v", err)
			}
			want, err := metricstest.LoadGolden(goldenDir, filepath.Base(path))
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range []struct {
				name string
				got  int
			}{
				{"files", got.files},
				{"sloc", got.sloc},
				{"largest_file_sloc", got.largestFileSLOC},
				{"exported_symbols", got.exportedSymbols},
			} {
				w, _ := want.Value(c.name)
				if float64(c.got) != w {
					t.Errorf("%s = %d, want %v", c.name, c.got, w)
				}
			}
			if got.largestFileSLOC > got.sloc {
				t.Errorf("largest_file_sloc %d > sloc %d", got.largestFileSLOC, got.sloc)
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
	l := loadForSize(b)
	for b.Loop() {
		for _, path := range l.paths {
			if _, err := size(l, l.pkgs[path]); err != nil {
				b.Fatal(err)
			}
		}
	}
}
