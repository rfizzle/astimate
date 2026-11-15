package inspect

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics/inventory"
)

// recordsFile parses a two-declaration file at dir/a.go and returns its
// file set and the positions of its declarations.
func recordsFile(t *testing.T, dir string) (*token.FileSet, []token.Pos) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(dir, "a.go"), "package p\n\ntype T struct{}\n\nfunc F() {}\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	return fset, []token.Pos{f.Decls[0].Pos(), f.Decls[1].Pos()}
}

func TestPositions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fset, ps := recordsFile(t, dir)
	tests := []struct {
		name string
		fset *token.FileSet
		ps   []token.Pos
		want []inventory.Position
	}{
		{name: "none", fset: fset, want: nil},
		{name: "relative file and line", fset: fset, ps: ps, want: []inventory.Position{{File: "a.go", Line: 3}, {File: "a.go", Line: 5}}},
		{name: "invalid position", fset: fset, ps: []token.Pos{token.NoPos, ps[1]}, want: []inventory.Position{{}, {File: "a.go", Line: 5}}},
		{name: "nil file set", ps: ps, want: []inventory.Position{{}, {}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Positions(tt.fset, dir, tt.ps); !slices.Equal(got, tt.want) {
				t.Errorf("Positions = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSymbols(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fset, ps := recordsFile(t, dir)
	if got := Symbols(fset, dir, nil); got != nil {
		t.Errorf("Symbols of none = %+v, want nil", got)
	}
	in := []Symbol{
		{Kind: "type", Name: "T", Pos: ps[0]},
		{Kind: "func", Receiver: "T", Name: "M", Pos: ps[1]},
		{Kind: "var", Name: "v"},
	}
	want := []inventory.Symbol{
		{Kind: "type", Name: "T", Position: inventory.Position{File: "a.go", Line: 3}},
		{Kind: "func", Receiver: "T", Name: "M", Position: inventory.Position{File: "a.go", Line: 5}},
		{Kind: "var", Name: "v"},
	}
	if got := Symbols(fset, dir, in); !slices.Equal(got, want) {
		t.Errorf("Symbols = %+v, want %+v", got, want)
	}
}
