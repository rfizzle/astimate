package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// copyTree copies the directory tree src into dst.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// fixtureCopy copies testdata/go/fixture and the extmod module its go.mod
// replaces into a temporary directory and returns the fixture's root.
func fixtureCopy(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	for _, name := range []string{"fixture", "extmod"} {
		copyTree(t, filepath.Join("..", "..", "testdata", "go", name), filepath.Join(tmp, name))
	}
	return filepath.Join(tmp, "fixture")
}

// goIn runs the go command in dir with the experiment environment.
func goIn(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go command not found")
	}
	return runGo(context.Background(), dir, append(defaultEnv(), "GOFLAGS=-mod=mod"), args...)
}

// TestStubFixturePackages stubs two tested packages of the fixture module
// and checks that the stub is deterministic, keeps the contract, compiles,
// and makes the package's own tests fail.
func TestStubFixturePackages(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go build and go test")
	}
	for _, dir := range []string{"tested", "dupes"} {
		t.Run(dir, func(t *testing.T) {
			root := fixtureCopy(t)
			pkgDir := filepath.Join(root, dir)
			if out, err := goIn(t, root, "test", "./"+dir); err != nil {
				t.Fatalf("fixture tests fail before stubbing: %v\n%s", err, out)
			}
			first, err := StubPackage(pkgDir)
			if err != nil {
				t.Fatal(err)
			}
			second, err := StubPackage(pkgDir)
			if err != nil {
				t.Fatal(err)
			}
			if TreeHash(first) != TreeHash(second) {
				t.Fatal("two stubs of the same tree differ")
			}
			for i := range first {
				if !bytes.Equal(first[i].Data, second[i].Data) {
					t.Fatalf("%s differs between runs", first[i].Name)
				}
				if strings.HasSuffix(first[i].Name, "_test.go") {
					t.Fatalf("stubbed a test file %s", first[i].Name)
				}
			}
			if err := WriteStub(pkgDir, first); err != nil {
				t.Fatal(err)
			}
			if out, err := goIn(t, root, "build", "./..."); err != nil {
				t.Fatalf("stubbed module does not build: %v\n%s", err, out)
			}
			out, err := goIn(t, root, "test", "./"+dir)
			if err == nil {
				t.Fatalf("tests pass on the stub:\n%s", out)
			}
			if testBuildBroken(out) || !strings.Contains(out, "not implemented") {
				t.Fatalf("tests should build and fail on the panic:\n%s", out)
			}
		})
	}
}

func TestStubSource(t *testing.T) {
	src := `//go:build !windows

// Package p is a sample.
package p

import (
	"fmt"
	"strconv"
	"strings"
)

// Limit bounds the input.
const Limit = 10

var names = strings.Fields("a b")

// T is a type.
type T struct{ n int }

// String renders t.
func (t *T) String() string {
	// inside the body
	return strconv.Itoa(t.n)
}

func init() { names = append(names, "c") }

// Map applies f; it is generic.
func Map[E any](s []E, f func(E) E) []E {
	out := make([]E, len(s))
	for i, v := range s {
		out[i] = f(v)
	}
	return out
}

//go:noescape
func asm(x int) int

func use() error { return fmt.Errorf("x") }
`
	want := `//go:build !windows

// Package p is a sample.
package p

import (
	"strings"
)

// Limit bounds the input.
const Limit = 10

var names = strings.Fields("a b")

// T is a type.
type T struct{ n int }

// String renders t.
func (t *T) String() string {
	panic("not implemented")
}

func init() {
	panic("not implemented")
}

// Map applies f; it is generic.
func Map[E any](s []E, f func(E) E) []E {
	panic("not implemented")
}

//go:noescape
func asm(x int) int

func use() error {
	panic("not implemented")
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "p.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := stubSource(path, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("stubSource:\n%s\nwant:\n%s", got, want)
	}
}

func TestStubPackageSkipsTestsAndSorts(t *testing.T) {
	dir := t.TempDir()
	for name, src := range map[string]string{
		"b.go":      "package p\n\nfunc B() int { return 1 }\n",
		"a.go":      "package p\n\nfunc A() int { return 2 }\n",
		"a_test.go": "package p\n\nfunc helper() int { return 3 }\n",
		"notes.txt": "not go",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := StubPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Name != "a.go" || files[1].Name != "b.go" {
		t.Fatalf("StubPackage files = %v, want a.go and b.go", files)
	}
	if _, err := StubPackage(t.TempDir()); err == nil {
		t.Fatal("StubPackage of an empty directory succeeded")
	}
}

func TestApplyStubChecksHash(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "p")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package p\n\nfunc A() int { return 2 }\n"
	if err := os.WriteFile(filepath.Join(pkg, "a.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &Experiment{Package: "example.com/m/p", Dir: "p", StubSHA256: strings.Repeat("0", 64)}
	if err := ApplyStub(root, e); err == nil {
		t.Fatal("ApplyStub accepted a wrong hash")
	}
	if data, _ := os.ReadFile(filepath.Join(pkg, "a.go")); string(data) != src {
		t.Fatal("ApplyStub wrote the stub despite the wrong hash")
	}
	files, err := StubPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	e.StubSHA256 = TreeHash(files)
	if err := ApplyStub(root, e); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(pkg, "a.go")); !strings.Contains(string(data), `panic("not implemented")`) {
		t.Fatalf("ApplyStub did not write the stub:\n%s", data)
	}
}
