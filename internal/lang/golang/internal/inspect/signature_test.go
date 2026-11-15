package inspect

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// sigOf parses src, a file declaring one function or method, and returns
// its signature hash.
func sigOf(tb testing.TB, src string) uint64 {
	tb.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "src.go", "package p\n"+src, 0)
	if err != nil {
		tb.Fatalf("parsing %q: %v", src, err)
	}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			return Signature(fn)
		}
	}
	tb.Fatalf("no function in %q", src)
	return 0
}

func TestSignature(t *testing.T) {
	cases := []struct {
		name         string
		before, head string
		same         bool
	}{
		{"parameter names", "func F(a string, b int) error { return nil }", "func F(x string, y int) (err error) { return }", true},
		{"grouped names", "func F(a, b int) {}", "func F(int, int) {}", true},
		{"body and comments", "func F(a int) int { return a }", "// F doubles.\nfunc F(a int) int { return a * 2 }", true},
		{"receiver pointer", "func (T) M(a int) {}", "func (*T) M(a int) {}", true},
		{"type parameter renamed", "func F[T any](v T) T { return v }", "func F[U any](v U) U { return v }", true},
		{"receiver type parameter renamed", "func (s *Set[T]) Add(v T) {}", "func (s *Set[E]) Add(v E) {}", true},
		{"nested func type parameter names", "func F(f func(ctx int) error) {}", "func F(f func(c int) error) {}", true},
		{"variadic added", "func SetLatest(name string) {}", "func SetLatest(name string, pin ...string) {}", false},
		{"variadic to slice", "func F(a ...string) {}", "func F(a []string) {}", false},
		{"parameter type", "func F(a int) {}", "func F(a int64) {}", false},
		{"parameter removed", "func F(a, b int) {}", "func F(a int) {}", false},
		{"result added", "func F() {}", "func F() error { return nil }", false},
		{"parameter moved to result", "func F(a int) {}", "func F() (a int) { return }", false},
		{"qualified type", "func F(a time.Duration) {}", "func F(a time.Time) {}", false},
		{"array length", "func F(a [4]byte) {}", "func F(a [8]byte) {}", false},
		{"channel direction", "func F(c chan int) {}", "func F(c <-chan int) {}", false},
		{"constraint", "func F[T any](v T) {}", "func F[T comparable](v T) {}", false},
		{"type parameter added", "func F[T any](v T) {}", "func F[T, U any](v T) {}", false},
		{"nested func type result", "func F(f func() error) {}", "func F(f func() (int, error)) {}", false},
		{"struct field name", "func F(a struct{ X int }) {}", "func F(a struct{ Y int }) {}", false},
		{"names run together", "func F(a ab, b c) {}", "func F(a a, b bc) {}", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, h := sigOf(t, tc.before), sigOf(t, tc.head)
			if b == 0 || h == 0 {
				t.Fatalf("Signature returned 0: %x, %x", b, h)
			}
			if (b == h) != tc.same {
				t.Errorf("Signature(%q) = %x, Signature(%q) = %x, want same %v", tc.before, b, tc.head, h, tc.same)
			}
		})
	}
}

// TestComplexitySignature checks that Complexity hashes the signature of
// exported functions and methods only.
func TestComplexitySignature(t *testing.T) {
	src := "package p\nfunc F(a int) {}\nfunc f(a int) {}\ntype t struct{}\nfunc (t) M() {}\nfunc (t) m() {}\n"
	for _, fc := range Complexity(nil, parseForComplexity(t, src)).PerFunc {
		if exported := ast.IsExported(fc.Ident); exported != (fc.Signature != 0) {
			t.Errorf("%s: Signature = %x, want non-zero %v", fc.Name, fc.Signature, exported)
		}
	}
}

func BenchmarkSignature(b *testing.B) {
	f, err := parser.ParseFile(token.NewFileSet(), "src.go",
		"package p\nfunc (s *Store[K, V]) SetLatest(ctx context.Context, key K, val map[string][]V, opts ...func(*options) error) (old V, ok bool, err error) { return }\n", 0)
	if err != nil {
		b.Fatal(err)
	}
	fn := f.Decls[0].(*ast.FuncDecl)
	for b.Loop() {
		Signature(fn)
	}
}
