package golang

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"golang.org/x/tools/go/packages"
)

// fixturePackages are the non-test packages of testdata/go/fixture, sorted.
func fixturePackages() []string {
	return []string{
		"example.com/fixture/a",
		"example.com/fixture/b",
		"example.com/fixture/dupes",
		"example.com/fixture/hidden",
		"example.com/fixture/hub",
		"example.com/fixture/tested",
		"example.com/fixture/trivial",
	}
}

// withLoadCounter replaces the extractor's load function with packages.Load
// wrapped in a counter.
func withLoadCounter(n *atomic.Int32) Option {
	return func(e *Extractor) {
		e.load = func(cfg *packages.Config, patterns ...string) ([]*packages.Package, error) {
			n.Add(1)
			return packages.Load(cfg, patterns...)
		}
	}
}

// brokenRoot returns the absolute path of testdata/go/broken.
func brokenRoot(t testing.TB) string {
	t.Helper()
	return filepath.Join(filepath.Dir(fixtureRoot(t)), "broken")
}

func writeFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

func TestLanguage(t *testing.T) {
	if got := New().Language(); got != "go" {
		t.Fatalf("Language() = %q, want %q", got, "go")
	}
}

func TestDetect(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  bool
	}{
		{
			name: "go.mod file",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/m\n")
			},
			want: true,
		},
		{
			name:  "empty directory",
			setup: func(*testing.T, string) {},
			want:  false,
		},
		{
			name: "go.mod is a directory",
			setup: func(t *testing.T, dir string) {
				if err := os.Mkdir(filepath.Join(dir, "go.mod"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
			want: false,
		},
		{
			name: "go.mod only in a subdirectory",
			setup: func(t *testing.T, dir string) {
				sub := filepath.Join(dir, "sub")
				if err := os.Mkdir(sub, 0o700); err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(sub, "go.mod"), "module example.com/m\n")
			},
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)
			if got := New().Detect(dir); got != tc.want {
				t.Fatalf("Detect = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReadModulePath(t *testing.T) {
	for _, tc := range []struct {
		name    string
		gomod   string // empty means no go.mod file
		want    string
		wantErr string
	}{
		{name: "plain", gomod: "module example.com/m\n\ngo 1.27\n", want: "example.com/m"},
		{name: "quoted", gomod: "module \"example.com/q\"\n", want: "example.com/q"},
		{
			name:  "comments and requires",
			gomod: "// header\nmodule example.com/c // trailing\n\ngo 1.27\n\nrequire example.com/x v1.0.0\n",
			want:  "example.com/c",
		},
		{name: "no module directive", gomod: "go 1.27\n", wantErr: "no module directive"},
		{name: "missing go.mod", wantErr: "reading module path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.gomod != "" {
				writeFile(t, filepath.Join(dir, "go.mod"), tc.gomod)
			}
			got, err := readModulePath(dir)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("readModulePath error = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("readModulePath: %v", err)
			}
			if got != tc.want {
				t.Fatalf("readModulePath = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPackagesFixture(t *testing.T) {
	got, err := New().Packages(fixtureRoot(t))
	if err != nil {
		t.Fatalf("Packages: %v", err)
	}
	if want := fixturePackages(); !slices.Equal(got, want) {
		t.Fatalf("Packages = %v, want %v", got, want)
	}
}

func TestLoadedIndexesTestPackages(t *testing.T) {
	root := fixtureRoot(t)
	e := New()
	if _, err := e.Packages(root); err != nil {
		t.Fatalf("Packages: %v", err)
	}
	l := e.modules[root].l
	if l.modulePath != "example.com/fixture" {
		t.Errorf("modulePath = %q, want example.com/fixture", l.modulePath)
	}
	if l.fset == nil {
		t.Error("fset is nil")
	}
	if len(l.pkgs) != len(l.paths) {
		t.Errorf("len(pkgs) = %d, len(paths) = %d", len(l.pkgs), len(l.paths))
	}
	for path, p := range l.pkgs {
		if p.Types == nil || p.TypesInfo == nil || len(p.Syntax) == 0 {
			t.Errorf("%s: missing types or syntax", path)
		}
		for _, f := range p.GoFiles {
			if strings.HasSuffix(f, "_test.go") {
				t.Errorf("%s: non-test package holds %s", path, f)
			}
		}
	}
	if got, want := keys(l.tests), []string{"example.com/fixture/dupes", "example.com/fixture/tested"}; !slices.Equal(got, want) {
		t.Errorf("tests keys = %v, want %v", got, want)
	}
	if got, want := keys(l.xtests), []string{"example.com/fixture/tested"}; !slices.Equal(got, want) {
		t.Errorf("xtests keys = %v, want %v", got, want)
	}
	if p := l.xtests["example.com/fixture/tested"]; p.PkgPath != "example.com/fixture/tested_test" {
		t.Errorf("xtest PkgPath = %q, want example.com/fixture/tested_test", p.PkgPath)
	}
	if l.reverse != nil {
		t.Error("reverse is filled before any metric builds it")
	}
}

func keys(m map[string]*packages.Package) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func TestOneLoadPerModule(t *testing.T) {
	var loads atomic.Int32
	e := New(withLoadCounter(&loads))
	root := fixtureRoot(t)
	for range 2 {
		if _, err := e.Packages(root); err != nil {
			t.Fatalf("Packages: %v", err)
		}
	}
	mod := &metrics.ModuleContext{Root: root, ModulePath: "example.com/fixture"}
	for _, pkg := range []string{"example.com/fixture/a", "example.com/fixture/hub", "example.com/fixture/tested"} {
		if _, err := e.Extract(t.Context(), mod, pkg); err != nil {
			t.Fatalf("Extract(%s): %v", pkg, err)
		}
	}
	if _, ok := mod.Cache.(*loaded); !ok {
		t.Errorf("mod.Cache = %T, want *loaded", mod.Cache)
	}
	// A second context whose root is spelled differently shares the load.
	sep := string(filepath.Separator)
	other := &metrics.ModuleContext{Root: root + sep + "a" + sep + ".."}
	if _, err := e.Extract(t.Context(), other, "example.com/fixture/b"); err != nil {
		t.Fatalf("Extract on second context: %v", err)
	}
	if other.Cache != mod.Cache {
		t.Error("second context got a different load")
	}
	if n := loads.Load(); n != 1 {
		t.Fatalf("packages.Load called %d times, want 1", n)
	}
}

func TestExtractFilesMatchesGolden(t *testing.T) {
	root := fixtureRoot(t)
	e := New()
	mod := &metrics.ModuleContext{Root: root, ModulePath: "example.com/fixture"}
	for _, pkg := range fixturePackages() {
		t.Run(pkg, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, "golden", filepath.Base(pkg)+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var want metrics.RawMetrics
			if err := json.Unmarshal(data, &want); err != nil {
				t.Fatal(err)
			}
			got, err := e.Extract(t.Context(), mod, pkg)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			if got.Files != want.Files {
				t.Errorf("files = %d, want %d", got.Files, want.Files)
			}
		})
	}
}

func TestExtractErrors(t *testing.T) {
	root := fixtureRoot(t)
	e := New()
	mod := &metrics.ModuleContext{Root: root}
	for _, pkg := range []string{"example.com/fixture/nope", "example.com/fixture/tested_test", "fmt", "example.com/fixture/tested.test"} {
		t.Run(pkg, func(t *testing.T) {
			_, err := e.Extract(t.Context(), mod, pkg)
			if !errors.Is(err, ErrUnknownPackage) {
				t.Fatalf("Extract error = %v, want ErrUnknownPackage", err)
			}
			if !strings.Contains(err.Error(), pkg) {
				t.Fatalf("error %q does not name %s", err, pkg)
			}
		})
	}
	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := e.Extract(ctx, mod, "example.com/fixture/a")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Extract error = %v, want context.Canceled", err)
		}
	})
}

func TestTypeErrorNamesPackage(t *testing.T) {
	root := brokenRoot(t)
	e := New()
	check := func(t *testing.T, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("no error for a module with a type error")
		}
		if !strings.Contains(err.Error(), "loading example.com/broken:") {
			t.Errorf("error %q does not name example.com/broken", err)
		}
		var pe packages.Error
		if !errors.As(err, &pe) {
			t.Errorf("error %v does not wrap a packages.Error", err)
		} else if pe.Kind != packages.TypeError {
			t.Errorf("packages.Error kind = %v, want TypeError", pe.Kind)
		}
	}
	t.Run("Packages", func(t *testing.T) {
		_, err := e.Packages(root)
		check(t, err)
	})
	t.Run("Extract", func(t *testing.T) {
		_, err := e.Extract(t.Context(), &metrics.ModuleContext{Root: root}, "example.com/broken")
		check(t, err)
	})
}

func TestNoPackages(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/empty\n\ngo 1.27\n")
	_, err := New().Packages(dir)
	if !errors.Is(err, ErrNoPackages) {
		t.Fatalf("Packages error = %v, want ErrNoPackages", err)
	}
}

func TestInModule(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"example.com/m", true},
		{"example.com/m/sub", true},
		{"example.com/m_test", false},
		{"example.com/mod", false},
		{"example.com", false},
		{"fmt", false},
	} {
		if got := inModule("example.com/m", tc.path); got != tc.want {
			t.Errorf("inModule(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func benchmarkLoad(b *testing.B, root string) {
	b.Helper()
	for b.Loop() {
		if _, err := New().Packages(root); err != nil {
			b.Fatalf("Packages(%s): %v", root, err)
		}
	}
}

func BenchmarkLoadFixture(b *testing.B) {
	benchmarkLoad(b, fixtureRoot(b))
}

func BenchmarkLoadSelf(b *testing.B) {
	benchmarkLoad(b, filepath.Join(fixtureRoot(b), "..", "..", ".."))
}
