package inspect

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
			got := Complexity(nil, parseForComplexity(t, tt.src))
			if got.MaxNesting != tt.maxNesting || got.FuncCount != tt.funcCount {
				t.Errorf("max_nesting=%d func_count=%d, want max_nesting=%d func_count=%d",
					got.MaxNesting, got.FuncCount, tt.maxNesting, tt.funcCount)
			}
		})
	}
}

func TestComplexityNoFunctions(t *testing.T) {
	got := Complexity(nil, parseForComplexity(t, "package p\nconst c = 1\n"))
	if got.CognitiveP90 != 0 || got.CognitiveTotal != 0 || got.FuncCount != 0 || got.MaxNesting != 0 {
		t.Errorf("got %+v, want all zero", got)
	}
}

func TestComplexitySingleFunctionP90(t *testing.T) {
	src := "package p\nfunc f(a, b bool, s []int) {\n\tfor range s {\n\t\tif a && b {\n\t\t}\n\t}\n}\n"
	got := Complexity(nil, parseForComplexity(t, src))
	if len(got.PerFunc) != 1 {
		t.Fatalf("perFunc = %+v, want one function", got.PerFunc)
	}
	// for +1, if +1 +1 nesting, && +1.
	if c := got.PerFunc[0].Cognitive; c != 4 || got.CognitiveP90 != c || got.CognitiveTotal != c {
		t.Errorf("cognitive=%d p90=%d total=%d, want all 4", c, got.CognitiveP90, got.CognitiveTotal)
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
	got := Complexity(l, l.Pkgs["example.com/fixture/hidden"])
	want := []Func{
		{Name: "init", Cognitive: 0, Nesting: 0},
		{Name: "Drain", Cognitive: 10, Nesting: 4},
		{Name: "init", Cognitive: 0, Nesting: 0},
	}
	if len(got.PerFunc) != len(want) {
		t.Fatalf("perFunc = %+v, want %+v", got.PerFunc, want)
	}
	for i, w := range want {
		g := got.PerFunc[i]
		if g.Name != w.Name || g.Cognitive != w.Cognitive || g.Nesting != w.Nesting {
			t.Errorf("perFunc[%d] = %+v, want %+v", i, g, w)
		}
		if pos := l.Fset.Position(g.Pos); filepath.Ext(pos.Filename) != ".go" || pos.Line == 0 {
			t.Errorf("perFunc[%d] at %s, want a line of a Go file", i, pos)
		}
	}
}

func TestFuncDeclName(t *testing.T) {
	src := "package p\ntype T struct{}\ntype G[A, B any] struct{}\ntype H[A any] struct{}\n" +
		"func f() {}\nfunc (T) v() {}\nfunc (*T) p() {}\nfunc (G[A, B]) g() {}\nfunc (*H[A]) h() {}\n"
	got := Complexity(nil, parseForComplexity(t, src))
	want := []string{"f", "T.v", "T.p", "G.g", "H.h"}
	if len(got.PerFunc) != len(want) {
		t.Fatalf("perFunc = %+v, want names %v", got.PerFunc, want)
	}
	for i, w := range want {
		if got.PerFunc[i].Name != w {
			t.Errorf("name[%d] = %q, want %q", i, got.PerFunc[i].Name, w)
		}
	}
}

func BenchmarkComplexityWalk(b *testing.B) {
	l := loadFixture(b)
	for b.Loop() {
		for _, path := range l.Paths {
			Complexity(l, l.Pkgs[path])
		}
	}
}

// fingerprintOf parses src, which declares exactly one function, and
// returns that function's fingerprint.
func fingerprintOf(t *testing.T, src string) uint64 {
	t.Helper()
	got := Complexity(nil, parseForComplexity(t, src))
	if len(got.PerFunc) != 1 {
		t.Fatalf("parsed %d functions, want 1", len(got.PerFunc))
	}
	return got.PerFunc[0].Fingerprint
}

func TestFingerprint(t *testing.T) {
	const base = `package p

// f sums the positive values of s.
func f(s []int) int {
	total := 0
	for _, v := range s {
		if v > 0 && v < 100 {
			total += v // accumulate
		}
	}
	return total
}
`
	tests := []struct {
		name string
		src  string
		same bool
	}{
		{name: "identical", src: base, same: true},
		{
			name: "comments only",
			src: `package p

// f adds up what is positive.
func f(s []int) int {
	/* running sum */
	total := 0
	for _, v := range s {
		// skip negatives
		if v > 0 && v < 100 {
			total += v
		}
	}
	return total // done
}
`,
			same: true,
		},
		{
			name: "layout",
			src: "package p\nfunc f(s []int) int { total := 0; for _, v := range s { if v > 0 && v < 100 { total += v } }\n" +
				"\n\n\treturn total }\n",
			same: true,
		},
		{
			name: "renamed variables and changed literal",
			src: `package p
func f(xs []int) int {
	sum := 0
	for _, x := range xs {
		if x > 1 && x < 50 {
			sum += x
		}
	}
	return sum
}
`,
			same: true,
		},
		{
			name: "signature only",
			src: `package p
func f(s []int, _ bool) int {
	total := 0
	for _, v := range s {
		if v > 0 && v < 100 {
			total += v
		}
	}
	return total
}
`,
			same: true,
		},
		{
			name: "operator changed",
			src: `package p
func f(s []int) int {
	total := 0
	for _, v := range s {
		if v > 0 || v < 100 {
			total += v
		}
	}
	return total
}
`,
		},
		{
			name: "assignment token changed",
			src: `package p
func f(s []int) int {
	total := 0
	for _, v := range s {
		if v > 0 && v < 100 {
			total -= v
		}
	}
	return total
}
`,
		},
		{
			name: "branch added",
			src: `package p
func f(s []int) int {
	total := 0
	for _, v := range s {
		if v > 0 && v < 100 {
			total += v
		} else {
			break
		}
	}
	return total
}
`,
		},
		{
			name: "statement moved out of the loop",
			src: `package p
func f(s []int) int {
	total := 0
	for _, v := range s {
		if v > 0 && v < 100 {
		}
		total += v
	}
	return total
}
`,
		},
	}
	want := fingerprintOf(t, base)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fingerprintOf(t, tt.src); (got == want) != tt.same {
				t.Errorf("fingerprint equal = %v, want %v", got == want, tt.same)
			}
		})
	}
}

func TestFingerprintOptionalChildren(t *testing.T) {
	// Pairs whose trees hold the same nodes in different optional slots.
	pairs := []struct{ name, a, b string }{
		{"slice bounds", "package p\nfunc f(a []int, i int) { _ = a[i:] }\n", "package p\nfunc f(a []int, i int) { _ = a[:i] }\n"},
		{"for clauses", "package p\nfunc f(i int) { for i++; ; {} }\n", "package p\nfunc f(i int) { for ; ; i++ {} }\n"},
		{"direct recursion", "package p\nfunc f(n int) int { return g(n) }\n", "package p\nfunc f(n int) int { return f(n) }\n"},
		{"variadic call", "package p\nfunc f(g func(...int), a []int) { g(a...) }\n", "package p\nfunc f(g func([]int), a []int) { g(a) }\n"},
	}
	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			if fingerprintOf(t, p.a) == fingerprintOf(t, p.b) {
				t.Error("fingerprints are equal, want them to differ")
			}
		})
	}
}

func BenchmarkInspectBody(b *testing.B) {
	l := loadFixture(b)
	var bodies []*ast.BlockStmt
	for _, path := range l.Paths {
		for _, f := range l.SourceSyntax(l.Pkgs[path]) {
			for _, d := range f.Decls {
				if fn, ok := d.(*ast.FuncDecl); ok {
					bodies = append(bodies, fn.Body)
				}
			}
		}
	}
	for b.Loop() {
		for _, body := range bodies {
			inspectBody("f", body)
		}
	}
}
