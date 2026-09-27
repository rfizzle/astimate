package golang

import (
	"context"
	"go/ast"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// fingerprintOf parses src, which declares exactly one function, and
// returns that function's fingerprint.
func fingerprintOf(t *testing.T, src string) uint64 {
	t.Helper()
	got := complexity(nil, parseForComplexity(t, src))
	if len(got.perFunc) != 1 {
		t.Fatalf("parsed %d functions, want 1", len(got.perFunc))
	}
	return got.perFunc[0].fingerprint
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
		return functionInfos(nil, "", complexity(nil, parseForComplexity(t, src)).perFunc)
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

func BenchmarkInspectBody(b *testing.B) {
	l := loadFixture(b)
	var bodies []*ast.BlockStmt
	for _, path := range l.paths {
		for _, f := range sourceSyntax(l, l.pkgs[path]) {
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
