package syntactic

import (
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// TestSyntacticRefs checks what a test file names of the loaded packages it
// imports: a selector on an import's name is a func of that package, a
// bare identifier one of a dot-imported package unless the file declares
// it, and any other exported selector, on a name that is no import's
// (other is not imported here), a method of every loaded package the file
// imports. Selectors on fmt, testing, math/rand/v2 and gopkg.in/yaml.v3
// name nothing.
func TestSyntacticRefs(t *testing.T) {
	t.Parallel()
	m := &load.Module{Pkgs: map[string]*packages.Package{
		"example.com/m/plain": {Name: "plain"},
		"example.com/m/v2":    {Name: "named"},
		"example.com/m/alias": {Name: "alias"},
		"example.com/m/dot":   {Name: "dot"},
		"example.com/m/blank": {Name: "blank"},
		"example.com/m/other": {Name: "other"},
	}}
	const src = `package x_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"gopkg.in/yaml.v3"

	"example.com/m/plain"
	"example.com/m/v2"
	al "example.com/m/alias"
	. "example.com/m/dot"
	_ "example.com/m/blank"
)

func TestX(t *testing.T) {
	plain.Run()
	named.Make().Close()
	al.Build()
	Dotted()
	plain.unexported()
	fmt.Println(other.Missing, rand.IntN(3))
	_, _ = yaml.Marshal(1)
}

type Helper struct{}
`
	tests := []struct {
		path           string
		funcs, methods []string
	}{
		{path: "example.com/m/plain", funcs: []string{"Run"}, methods: []string{"Close", "Missing"}},
		{path: "example.com/m/v2", funcs: []string{"Make"}, methods: []string{"Close", "Missing"}},
		{path: "example.com/m/alias", funcs: []string{"Build"}, methods: []string{"Close", "Missing"}},
		{path: "example.com/m/dot", funcs: []string{"Dotted"}, methods: []string{"Close", "Missing"}},
		{path: "example.com/m/blank", methods: []string{"Close", "Missing"}},
	}
	f, err := parser.ParseFile(token.NewFileSet(), "x_test.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	byPath := make(map[string]*names)
	refs(m, f, byPath)
	if _, ok := byPath["example.com/m/other"]; ok {
		t.Error("a package the file does not import is referenced")
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			n := byPath[tc.path]
			if n == nil {
				t.Fatal("no references")
			}
			if got := slices.Sorted(maps.Keys(n.funcs)); !slices.Equal(got, tc.funcs) {
				t.Errorf("funcs = %v, want %v", got, tc.funcs)
			}
			if got := slices.Sorted(maps.Keys(n.methods)); !slices.Equal(got, tc.methods) {
				t.Errorf("methods = %v, want %v", got, tc.methods)
			}
		})
	}
}
