package golang

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/inspect"
	"github.com/rfizzle/astimate/internal/metrics"
	"golang.org/x/tools/go/packages"
)

// parseForComplexity parses src as one file and wraps it in a package that
// carries only syntax, which is all complexity reads.
func parseForComplexity(t *testing.T, src string) *packages.Package {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "src.go", src, 0)
	if err != nil {
		t.Fatalf("parsing snippet: %v", err)
	}
	return &packages.Package{Syntax: []*ast.File{f}}
}

// TestFunctionMatching runs the baseline diff over two versions of a
// package's source: a comment-only edit is unchanged, a rename and a
// receiver change are new, and a deleted function is not reported.
func TestFunctionMatching(t *testing.T) {
	const before = `package p

type T struct{}
type U struct{}

// Parse is documented.
func Parse(s string) int {
	if s == "" {
		return 0
	}
	return len(s)
}

func Old() {}

func (T) Run(n int) int {
	for n > 0 {
		n--
	}
	return n
}

func (*T) Stop() {}

func helper(a, b bool) bool { return a && b }
`
	const after = `package p

type T struct{}
type U struct{}

// Parse is documented differently now.
func Parse(s string) int {
	// An empty string has length zero.
	if s == "" {
		return 0
	}
	return len(s) // unchanged
}

func (U) Run(n int) int {
	for n > 0 {
		n--
	}
	return n
}

func (T) Stop() {}

func helperRenamed(a, b bool) bool { return a && b }
`
	infos := func(src string) []metrics.FunctionInfo {
		return functionInfos(nil, "", inspect.Complexity(nil, parseForComplexity(t, src)).PerFunc)
	}
	var got []string
	for _, f := range metrics.ChangedFunctions(infos(before), infos(after)) {
		got = append(got, f.QualifiedName())
	}
	// Stop moved from *T to T: the receiver's base type is the key, so it
	// matches. Old was deleted and is not reported.
	if want := []string{"U.Run", "helperRenamed"}; !slices.Equal(got, want) {
		t.Errorf("changed = %q, want %q", got, want)
	}
}

func TestExtractorFunctions(t *testing.T) {
	e := New()
	mod := &metrics.ModuleContext{Root: fixtureRoot(t), ModulePath: "example.com/fixture"}
	fns, err := e.Functions(context.Background(), mod, "example.com/fixture/hidden")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for i := range fns {
		names = append(names, fns[i].QualifiedName())
		if fns[i].File == "" || fns[i].Line == 0 {
			t.Errorf("%s at %q:%d, want a file and line", names[i], fns[i].File, fns[i].Line)
		}
	}
	if want := []string{"init", "Drain", "init"}; !slices.Equal(names, want) {
		t.Errorf("functions = %q, want %q", names, want)
	}
	if _, err := e.Functions(context.Background(), mod, "example.com/fixture/missing"); err == nil {
		t.Error("Functions of an unknown package succeeded, want an error")
	}
}
