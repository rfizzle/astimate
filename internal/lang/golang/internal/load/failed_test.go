package load

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// offline keeps go list from reaching the network for a missing module.
func offline(t *testing.T) {
	t.Helper()
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
}

// skippedRoot returns the absolute path of testdata/go/skipped, a module
// whose package remote imports a module go.mod does not require.
func skippedRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(filepath.Dir(fixtureRoot(t)), "skipped")
}

// failingModule writes a module example.com/fail with one loading package,
// ok, and one failing package per entry of broken (directory name to
// files), and returns its root.
func failingModule(t *testing.T, broken map[string]map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/fail\n\ngo 1.27\n")
	pkgs := map[string]map[string]string{"ok": {"ok.go": "package ok\n\n// V is one.\nfunc V() int { return 1 }\n"}}
	for dir, files := range broken {
		pkgs[dir] = files
	}
	for dir, files := range pkgs {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			t.Fatal(err)
		}
		for name, src := range files {
			writeFile(t, filepath.Join(root, dir, name), src)
		}
	}
	return root
}

func TestLoadSkipsFailingPackages(t *testing.T) {
	offline(t)
	for _, tc := range []struct {
		name       string
		files      map[string]string
		wantReason string
		wantErr    string
	}{
		{
			name:       "missing module",
			files:      map[string]string{"x.go": "package x\n\nimport \"example.com/absent/y\"\n\nvar _ = y.Z\n"},
			wantReason: "missing module",
			wantErr:    "example.com/absent/y",
		},
		{
			name:       "type error",
			files:      map[string]string{"x.go": "package x\n\nvar _ = undefinedName\n"},
			wantReason: "type error",
			wantErr:    "undefinedName",
		},
		{
			name:       "syntax error",
			files:      map[string]string{"x.go": "package x\n\nfunc {\n"},
			wantReason: "syntax error",
			wantErr:    "x.go:3",
		},
		{
			name: "test files only",
			files: map[string]string{
				"x.go":      "package x\n\n// F is two.\nfunc F() int { return 2 }\n",
				"x_test.go": "package x\n\nvar _ = undefinedInTest\n",
			},
			wantReason: "type error in test files",
			wantErr:    "undefinedInTest",
		},
		{
			name: "missing module in an external test",
			files: map[string]string{
				"x.go":      "package x\n",
				"x_test.go": "package x_test\n\nimport \"example.com/absent/y\"\n\nvar _ = y.Z\n",
			},
			wantReason: "missing module in test files",
			wantErr:    "example.com/absent/y",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := failingModule(t, map[string]map[string]string{"x": tc.files})
			m, err := Load(&packages.Config{Dir: root}, packages.Load)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if want := []string{"example.com/fail/ok"}; !slices.Equal(m.Paths, want) {
				t.Errorf("paths = %v, want %v", m.Paths, want)
			}
			if len(m.SkippedPackages) != 1 {
				t.Fatalf("skipped = %+v, want x alone", m.SkippedPackages)
			}
			s := m.SkippedPackages[0]
			if s.Package != "example.com/fail/x" || s.Reason != tc.wantReason {
				t.Errorf("skipped = %q (%q), want example.com/fail/x (%q)", s.Package, s.Reason, tc.wantReason)
			}
			if s.Err == nil || !strings.Contains(s.Err.Error(), tc.wantErr) ||
				!strings.HasPrefix(s.Err.Error(), "loading example.com/fail/x") {
				t.Errorf("error = %v, want one naming the package and containing %q", s.Err, tc.wantErr)
			}
			for _, idx := range []map[string]*packages.Package{m.Pkgs, m.Tests, m.XTests} {
				if _, ok := idx["example.com/fail/x"]; ok {
					t.Error("the skipped package or a test variant of it is indexed")
				}
			}
		})
	}
}

func TestLoadAllFailing(t *testing.T) {
	offline(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/fail\n\ngo 1.27\n")
	writeFile(t, filepath.Join(root, "x.go"), "package x\n\nvar _ = undefinedName\n")
	_, err := Load(&packages.Config{Dir: root}, packages.Load)
	if err == nil || !strings.Contains(err.Error(), "undefinedName") {
		t.Fatalf("Load error = %v, want the one package's error", err)
	}
}

func TestGraphPackagesKeepsSkipped(t *testing.T) {
	offline(t)
	m := loadRoot(t, skippedRoot(t))
	if want := []string{"example.com/skipped/app", "example.com/skipped/base"}; !slices.Equal(m.Paths, want) {
		t.Errorf("paths = %v, want %v", m.Paths, want)
	}
	if len(m.SkippedPackages) != 1 || m.SkippedPackages[0].Package != "example.com/skipped/remote" ||
		m.SkippedPackages[0].Reason != "missing module" {
		t.Fatalf("skipped = %+v, want remote, missing module", m.SkippedPackages)
	}
	if len(m.Skipped) != 0 {
		t.Errorf("skipped directories = %+v, want none: remote was listed", m.Skipped)
	}
	g := m.GraphPackages()
	remote, ok := g["example.com/skipped/remote"]
	if len(g) != 3 || !ok || remote.Pkg == nil {
		t.Fatalf("graph packages = %v, want app, base and remote", slices.Sorted(maps.Keys(g)))
	}
	if _, ok := remote.Pkg.Imports["example.com/skipped/base"]; !ok {
		t.Errorf("remote's imports = %v, want base among them", remote.Pkg.Imports)
	}
}
