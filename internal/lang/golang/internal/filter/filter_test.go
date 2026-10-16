package filter

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/exclude"
	"golang.org/x/tools/go/packages"
)

// hand returns a package of the files named, each parsed from a one-line
// source, under root.
func hand(t *testing.T, fset *token.FileSet, root string, names ...string) *packages.Package {
	t.Helper()
	p := &packages.Package{PkgPath: "example.com/m/p"}
	for _, n := range names {
		abs := filepath.Join(root, filepath.FromSlash(n))
		f, err := parser.ParseFile(fset, abs, "package p\n", 0)
		if err != nil {
			t.Fatal(err)
		}
		p.GoFiles = append(p.GoFiles, abs)
		p.CompiledGoFiles = append(p.CompiledGoFiles, abs)
		p.Syntax = append(p.Syntax, f)
	}
	return p
}

// treeNames returns the base names of the files of trees.
func treeNames(fset *token.FileSet, trees []*ast.File) []string {
	var out []string
	for _, f := range trees {
		out = append(out, filepath.Base(fset.File(f.FileStart).Name()))
	}
	return out
}

func TestPackage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	list, err := exclude.New([]string{"p/b.go", "**/*_gen.go", "p/*_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		files    []string
		wantKept []string
		wantGone string
	}{
		{"nothing excluded", []string{"p/a.go", "q/a_test.go"}, []string{"a.go", "a_test.go"}, ""},
		{"some excluded", []string{"p/a.go", "p/x_gen.go"}, []string{"a.go"}, ""},
		{"every non-test file, first pattern in list order", []string{"p/x_gen.go", "p/b.go", "p/k_test.go"}, nil, "p/b.go"},
		{"a test file left alone does not keep the package", []string{"q/x_gen.go", "q/keep_test.go"}, nil, "**/*_gen.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			f := New(root, list, fset)
			p := hand(t, fset, root, tt.files...)
			out, gone := f.Package(p)
			if gone != tt.wantGone {
				t.Errorf("gone = %q, want %q", gone, tt.wantGone)
			}
			if tt.wantGone != "" {
				return
			}
			if got := treeNames(fset, out.Syntax); !slices.Equal(got, tt.wantKept) {
				t.Errorf("Syntax = %v, want %v", got, tt.wantKept)
			}
			if len(out.GoFiles) != len(tt.wantKept) || len(out.CompiledGoFiles) != len(tt.wantKept) {
				t.Errorf("GoFiles %v, CompiledGoFiles %v, want %d each", out.GoFiles, out.CompiledGoFiles, len(tt.wantKept))
			}
			if len(tt.wantKept) == len(tt.files) && out != p {
				t.Error("a package with nothing excluded was copied")
			}
		})
	}
}

func TestTreesAndFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	list, err := exclude.New([]string{"gen"})
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	f := New(root, list, fset)
	p := hand(t, fset, root, "gen/z.go", "gen/a.go", "lib/a.go")
	if got := treeNames(fset, f.Trees(p.Syntax, false)); !slices.Equal(got, []string{"a.go"}) {
		t.Errorf("Trees = %v, want lib's a.go only", got)
	}
	want := []File{{Rel: "gen/a.go", Pattern: "gen"}, {Rel: "gen/z.go", Pattern: "gen"}}
	if got := f.Files(); !slices.Equal(got, want) {
		t.Errorf("Files = %+v, want %+v", got, want)
	}
	outside := hand(t, fset, t.TempDir(), "gen/a.go")
	if out, gone := f.Package(outside); out != outside || gone != "" {
		t.Errorf("a package outside the root was filtered: gone %q", gone)
	}
	if !IsTest("a_test.go") || IsTest("a.go") {
		t.Error("IsTest is wrong")
	}
}
