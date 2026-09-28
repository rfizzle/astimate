package stub

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeModule writes files, by slash path, into a new module
// example.com/z and returns its root.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files["go.mod"] = "module example.com/z\n\ngo 1.27\n"
	for name, src := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// zeroSrc is a package whose initializers call its own code in every way
// the stub handles, next to initializers it must keep.
const zeroSrc = `// Package p has initializers.
package p

import (
	"regexp"
	"strings"
)

// Limit is a literal and keeps its value.
var limit = 10

// names calls another package only.
var names = strings.Fields("a b")

var pattern = regexp.MustCompile(expr())

// Default calls a package function.
var Default = compile("x") // trailing comment

var (
	cfg      Config = load()
	a, b            = pair()
	handlers        = []func(){func() { use() }}
	eager           = func() int { return count() }()
	since           = elapsed()
	generic         = Map[int]([]int{1})
	method          = Config{}.Name()
)

func init() {
	names = append(names, "c")
	Default = compile("y")
	register(func() { use() })
	if limit > 0 {
		register(use)
	}
}
`

// zeroOther declares what zeroSrc calls; its time import is one zeroSrc
// needs for the inferred type of since.
const zeroOther = `package p

import "time"

type matcher struct{}

// Config is configuration.
type Config struct{ N int }

// Name names c.
func (c Config) Name() string { return "c" }

func compile(s string) *matcher { return &matcher{} }
func load() Config               { return Config{} }
func pair() (int, string)        { return 1, "x" }
func use()                       {}
func register(f func())          {}
func count() int                 { return 1 }
func expr() string               { return "x" }
func elapsed() time.Duration     { return time.Second }

// Map returns s.
func Map[E any](s []E) []E { return s }
`

// zeroWant is zeroSrc stubbed: every initializer that calls the package
// loses its value and keeps or gains its type; the others are kept.
const zeroWant = `// Package p has initializers.
package p

import (
	"regexp"
	"strings"
	"time"
)

// Limit is a literal and keeps its value.
var limit = 10

// names calls another package only.
var names = strings.Fields("a b")

var pattern *regexp.Regexp

// Default calls a package function.
var Default *matcher // trailing comment

var (
	cfg      Config
	a        int
	b        string
	handlers = []func(){func() { use() }}
	eager    int
	since    time.Duration
	generic  []int
	method   string
)

func init() {
	names = append(names, "c")
}
`

// TestZeroInitializers stubs a package whose initializers call its own
// functions and methods, and checks the exact result: those initializers
// are removed and their types written, the others are kept, the init
// statements that call the package are removed and the rest kept, the
// import a type needs is added, and the stub builds and starts.
func TestZeroInitializers(t *testing.T) {
	if testing.Short() {
		t.Skip("type-checks with go/packages and runs go test")
	}
	root := writeModule(t, map[string]string{
		"p/p.go":      zeroSrc,
		"p/other.go":  zeroOther,
		"p/p_test.go": "package p\n\nimport \"testing\"\n\nfunc TestCount(t *testing.T) {\n\tif count() != 1 {\n\t\tt.Fatal()\n\t}\n}\n",
	})
	files, err := Package(t.Context(), filepath.Join(root, "p"), testEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[1].Name != "p.go" {
		t.Fatalf("files = %v", files)
	}
	if got := string(files[1].Data); got != zeroWant {
		t.Fatalf("p.go stubbed:\n%s\nwant:\n%s", got, zeroWant)
	}
	if s := Summarize(files); s.Inits != 3 || s.Vars != 8 {
		t.Fatalf("Summarize() = %+v, want 3 init statements and 8 variable specs", s)
	}
	if err := Write(filepath.Join(root, "p"), files); err != nil {
		t.Fatal(err)
	}
	if out, err := goIn(t, root, "vet", "./..."); err != nil {
		t.Fatalf("stub does not vet: %v\n%s", err, out)
	}
	if out, err := goIn(t, root, "test", "-count=1", "-run", "^$", "./p"); err != nil {
		t.Fatalf("stubbed tests do not start: %v\n%s", err, out)
	}
	if out, err := goIn(t, root, "test", "-count=1", "./p"); err == nil || !strings.Contains(out, Panic) {
		t.Fatalf("stubbed tests should fail on the panic: %v\n%s", err, out)
	}
}

// TestTreeNames checks that Tree names each file by its path below the
// tree, sorts them, and refuses a directory outside the tree.
func TestTreeNames(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"t/a.go", "t/sub/b.go", "t/sub/deep/c.go", "t/z.go"} {
		path := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		src := "package " + filepath.Base(filepath.Dir(path)) + "\n\nfunc F() int { return 1 }\n"
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A directory of tests only is a package with nothing to stub.
	if err := os.MkdirAll(filepath.Join(root, "t", "e2e"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "t", "e2e", "x_test.go"), []byte("package e2e\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := Tree(t.Context(), root, "t", []string{"t", "t/e2e", "t/sub/deep", "t/sub"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	if want := "a.go sub/b.go sub/deep/c.go z.go"; strings.Join(names, " ") != want {
		t.Fatalf("Tree names = %v, want %s", names, want)
	}
	if nf, ok := errors.AsType[*NoFilesError](func() error { _, err := Package(t.Context(), filepath.Join(root, "t", "e2e"), nil); return err }()); !ok || nf.Dir != filepath.Join(root, "t", "e2e") {
		t.Fatalf("Package of a test-only directory: %v", nf)
	}
	if _, err := Tree(t.Context(), root, "t/sub", []string{"t"}, nil); err == nil {
		t.Fatal("Tree stubbed a directory outside the tree")
	}
}

// TestKeepLiteralInitializers checks that a package whose initializers
// are literals or call other packages only is stubbed file by file
// exactly as before initializers were considered: no type-check, no
// variable touched.
func TestKeepLiteralInitializers(t *testing.T) {
	src := "package q\n\nimport \"errors\"\n\n// ErrX is an error.\nvar ErrX = errors.New(\"x\")\n\n" +
		"var table = map[string]int{\"a\": 1}\n\nvar conv = []byte(\"x\")\n\nfunc f() int { return table[\"a\"] }\n"
	// No go.mod: a type-check would fail, so success proves none ran.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "q.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := Package(t.Context(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, err := source(filepath.Join(dir, "q.go"), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if string(files[0].Data) != string(want) || Summarize(files) != (Summary{}) {
		t.Fatalf("stub changed a literal initializer:\n%s", files[0].Data)
	}
	for _, keep := range []string{`var ErrX = errors.New("x")`, `var table = map[string]int{"a": 1}`, `var conv = []byte("x")`} {
		if !strings.Contains(string(want), keep) {
			t.Fatalf("stub lost %q:\n%s", keep, want)
		}
	}
}
