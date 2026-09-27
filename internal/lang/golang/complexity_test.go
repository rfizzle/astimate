package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

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

func TestComplexityNesting(t *testing.T) {
	tests := []struct {
		name       string
		src        string
		maxNesting int
		funcCount  int
	}{
		{
			name:       "empty body",
			src:        "package p\nfunc f() {}\n",
			funcCount:  1,
			maxNesting: 0,
		},
		{
			name:       "if",
			src:        "package p\nfunc f(b bool) {\n\tif b {\n\t}\n}\n",
			funcCount:  1,
			maxNesting: 1,
		},
		{
			name:       "for",
			src:        "package p\nfunc f() {\n\tfor {\n\t\tbreak\n\t}\n}\n",
			funcCount:  1,
			maxNesting: 1,
		},
		{
			name:       "range",
			src:        "package p\nfunc f(s []int) {\n\tfor range s {\n\t}\n}\n",
			funcCount:  1,
			maxNesting: 1,
		},
		{
			name:       "switch with case clauses adds one level",
			src:        "package p\nfunc f(x int) {\n\tswitch x {\n\tcase 1:\n\tdefault:\n\t}\n}\n",
			funcCount:  1,
			maxNesting: 1,
		},
		{
			name:       "type switch",
			src:        "package p\nfunc f(x any) {\n\tswitch x.(type) {\n\tcase int:\n\t}\n}\n",
			funcCount:  1,
			maxNesting: 1,
		},
		{
			name:       "select",
			src:        "package p\nfunc f(c chan int) {\n\tselect {\n\tcase <-c:\n\t}\n}\n",
			funcCount:  1,
			maxNesting: 1,
		},
		{
			name:       "func literal at top level",
			src:        "package p\nfunc f() {\n\tg := func() {}\n\tg()\n}\n",
			funcCount:  1,
			maxNesting: 1,
		},
		{
			name: "func literal inside if continues from enclosing depth",
			src: "package p\nfunc f(b bool, s []int) {\n\tif b {\n\t\tg := func() {\n" +
				"\t\t\tfor range s {\n\t\t\t}\n\t\t}\n\t\tg()\n\t}\n}\n",
			funcCount:  1,
			maxNesting: 3,
		},
		{
			name:       "else block adds nothing",
			src:        "package p\nfunc f(b bool) {\n\tif b {\n\t} else {\n\t}\n}\n",
			funcCount:  1,
			maxNesting: 1,
		},
		{
			name:       "else if nests one level per IfStmt",
			src:        "package p\nfunc f(a, b bool) {\n\tif a {\n\t} else if b {\n\t}\n}\n",
			funcCount:  1,
			maxNesting: 2,
		},
		{
			name:       "siblings do not stack",
			src:        "package p\nfunc f(a, b bool) {\n\tif a {\n\t}\n\tif b {\n\t}\n}\n",
			funcCount:  1,
			maxNesting: 1,
		},
		{
			name: "maximum over functions and methods",
			src: "package p\ntype T struct{}\nfunc (T) m(b bool) {\n\tif b {\n\t\tfor {\n\t\t}\n\t}\n}\n" +
				"func init() {}\n",
			funcCount:  2,
			maxNesting: 2,
		},
		{
			name: "no functions",
			src:  "package p\nvar x = func() int {\n\tif true {\n\t\treturn 1\n\t}\n\treturn 0\n}()\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := complexity(nil, parseForComplexity(t, tt.src))
			if got.maxNesting != tt.maxNesting || got.funcCount != tt.funcCount {
				t.Errorf("max_nesting=%d func_count=%d, want max_nesting=%d func_count=%d",
					got.maxNesting, got.funcCount, tt.maxNesting, tt.funcCount)
			}
		})
	}
}

func TestComplexityNoFunctions(t *testing.T) {
	got := complexity(nil, parseForComplexity(t, "package p\nconst c = 1\n"))
	if got.cognitiveP90 != 0 || got.cognitiveTotal != 0 || got.funcCount != 0 || got.maxNesting != 0 {
		t.Errorf("got %+v, want all zero", got)
	}
}

func TestComplexitySingleFunctionP90(t *testing.T) {
	src := "package p\nfunc f(a, b bool, s []int) {\n\tfor range s {\n\t\tif a && b {\n\t\t}\n\t}\n}\n"
	got := complexity(nil, parseForComplexity(t, src))
	if len(got.perFunc) != 1 {
		t.Fatalf("perFunc = %+v, want one function", got.perFunc)
	}
	// for +1, if +1 +1 nesting, && +1.
	if c := got.perFunc[0].cognitive; c != 4 || got.cognitiveP90 != c || got.cognitiveTotal != c {
		t.Errorf("cognitive=%d p90=%d total=%d, want all 4", c, got.cognitiveP90, got.cognitiveTotal)
	}
}

func TestComplexityP90(t *testing.T) {
	seq := func(n int) []int {
		s := make([]int, n)
		for i := range s {
			// Reverse order so p90 must sort.
			s[i] = n - i
		}
		return s
	}
	tests := []struct {
		name   string
		scores []int
		want   int
	}{
		{"empty", nil, 0},
		{"one", []int{7}, 7},
		{"two takes the larger", []int{9, 3}, 9},
		{"ten takes rank 9", seq(10), 9},
		{"hundred takes rank 90", seq(100), 90},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p90(tt.scores); got != tt.want {
				t.Errorf("p90 = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestComplexityHiddenPerFunc(t *testing.T) {
	l := loadFixture(t)
	got := complexity(l, l.pkgs["example.com/fixture/hidden"])
	want := []funcComplexity{
		{name: "init", cognitive: 0, nesting: 0},
		{name: "Drain", cognitive: 10, nesting: 4},
		{name: "init", cognitive: 0, nesting: 0},
	}
	if len(got.perFunc) != len(want) {
		t.Fatalf("perFunc = %+v, want %+v", got.perFunc, want)
	}
	for i, w := range want {
		g := got.perFunc[i]
		if g.name != w.name || g.cognitive != w.cognitive || g.nesting != w.nesting {
			t.Errorf("perFunc[%d] = %+v, want %+v", i, g, w)
		}
		if pos := l.fset.Position(g.pos); filepath.Ext(pos.Filename) != ".go" || pos.Line == 0 {
			t.Errorf("perFunc[%d] at %s, want a line of a Go file", i, pos)
		}
	}
}

func TestFuncDeclName(t *testing.T) {
	src := "package p\ntype T struct{}\ntype G[A, B any] struct{}\ntype H[A any] struct{}\n" +
		"func f() {}\nfunc (T) v() {}\nfunc (*T) p() {}\nfunc (G[A, B]) g() {}\nfunc (*H[A]) h() {}\n"
	got := complexity(nil, parseForComplexity(t, src))
	want := []string{"f", "T.v", "T.p", "G.g", "H.h"}
	if len(got.perFunc) != len(want) {
		t.Fatalf("perFunc = %+v, want names %v", got.perFunc, want)
	}
	for i, w := range want {
		if got.perFunc[i].name != w {
			t.Errorf("name[%d] = %q, want %q", i, got.perFunc[i].name, w)
		}
	}
}

func BenchmarkComplexityWalk(b *testing.B) {
	l := loadFixture(b)
	for b.Loop() {
		for _, path := range l.paths {
			complexity(l, l.pkgs[path])
		}
	}
}
