package baseline

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/lang/typescript"
)

// packageDirs returns the sorted, de-duplicated package directories that
// sourceFiles finds among paths with the Go extractor's rules.
func packageDirs(paths []string, prefix string) []string {
	files := sourceFiles(paths, prefix, golang.New())
	dirs := make([]string, 0, len(files.dirs))
	for _, d := range files.dirs {
		dirs = append(dirs, d.pkg)
	}
	return slices.Compact(dirs)
}

func TestPackageDirs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		paths  []string
		prefix string
		want   []string
	}{
		{name: "test file selects its package", paths: []string{"a/a_test.go"}, prefix: ".", want: []string{"a"}},
		{name: "root package is dot", paths: []string{"main.go"}, prefix: ".", want: []string{"."}},
		{name: "non-Go files select nothing", paths: []string{"README.md", "a/data.json", "go.mod", "a/x.go.txt"}, prefix: ".", want: []string{}},
		{name: "duplicates collapse and sort", paths: []string{"b/b.go", "a/a.go", "b/b_test.go", "a/a.go"}, prefix: ".", want: []string{"a", "b"}},
		{name: "testdata ignored at any depth", paths: []string{"testdata/x.go", "a/testdata/y/y.go", "a/a.go"}, prefix: ".", want: []string{"a"}},
		{name: "module below repository root", paths: []string{"sub/a/a.go", "sub/root.go", "other/o.go", "subway/s.go"}, prefix: "sub", want: []string{".", "a"}},
		{name: "prefix is cleaned", paths: []string{"sub/a/a.go"}, prefix: "sub/", want: []string{"a"}},
		{name: "no paths", paths: nil, prefix: ".", want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := packageDirs(tt.paths, tt.prefix)
			if !slices.Equal(got, tt.want) {
				t.Errorf("packageDirs(%q, %q) = %q, want %q", tt.paths, tt.prefix, got, tt.want)
			}
		})
	}
}

// newChangeRepo commits a module with packages a and b, a README, a Go file
// under testdata and a nested module on master, and returns the repository
// and the commit hash.
func newChangeRepo(t *testing.T) (*testRepo, string) {
	t.Helper()
	r := newRepo(t, "master")
	r.write("go.mod", "module example.com/m\n\ngo 1.27\n")
	r.write("README.md", "readme\n")
	r.write("a/a.go", "package a\n")
	r.write("a/a_test.go", "package a\n")
	r.write("b/b.go", "package b\n")
	r.write("b/b_test.go", "package b\n")
	r.write("testdata/t/t.go", "package t\n")
	r.write("nested/go.mod", "module example.com/nested\n\ngo 1.27\n")
	r.write("nested/n/n.go", "package n\n")
	return r, r.commit("base")
}

func TestChangedPackages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		change      func(r *testRepo)
		wantPkgs    []string
		wantDeleted []string
	}{
		{
			name:   "no change",
			change: func(*testRepo) {},
		},
		{
			name:     "unstaged edit to a test file",
			change:   func(r *testRepo) { r.write("a/a_test.go", "package a\n\n// edited\n") },
			wantPkgs: []string{"a"},
		},
		{
			name: "committed edit to a test file",
			change: func(r *testRepo) {
				r.write("a/a_test.go", "package a\n\n// edited\n")
				r.commit("edit test")
			},
			wantPkgs: []string{"a"},
		},
		{
			name:   "non-Go file only",
			change: func(r *testRepo) { r.write("README.md", "changed\n") },
		},
		{
			name:     "untracked new package",
			change:   func(r *testRepo) { r.write("c/c.go", "package c\n") },
			wantPkgs: []string{"c"},
		},
		{
			name: "ignored file is not untracked",
			change: func(r *testRepo) {
				r.write(".gitignore", "gen/\n")
				r.write("gen/g.go", "package gen\n")
			},
		},
		{
			name: "staged but uncommitted change",
			change: func(r *testRepo) {
				r.write("b/b.go", "package b\n\n// staged\n")
				r.git("add", "b/b.go")
			},
			wantPkgs: []string{"b"},
		},
		{
			name: "every file of a package deleted",
			change: func(r *testRepo) {
				r.git("rm", "-q", "b/b.go", "b/b_test.go")
				r.commit("drop b")
			},
			wantDeleted: []string{"b"},
		},
		{
			name: "package renamed",
			change: func(r *testRepo) {
				r.git("mv", "b", "bee")
				r.commit("rename b")
			},
			wantPkgs:    []string{"bee"},
			wantDeleted: []string{"b"},
		},
		{
			name: "testdata and nested module ignored",
			change: func(r *testRepo) {
				r.write("testdata/t/t.go", "package t\n\n// edited\n")
				r.write("nested/n/n.go", "package n\n\n// edited\n")
				r.write("nested/new.go", "package nested\n")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, base := newChangeRepo(t)
			tt.change(r)
			got, err := ChangedPackages(context.Background(), r.dir, base, golang.New(), Head{})
			if err != nil {
				t.Fatalf("ChangedPackages: %v", err)
			}
			if !slices.Equal(got.Packages, tt.wantPkgs) {
				t.Errorf("Packages = %q, want %q", got.Packages, tt.wantPkgs)
			}
			if !slices.Equal(got.Deleted, tt.wantDeleted) {
				t.Errorf("Deleted = %q, want %q", got.Deleted, tt.wantDeleted)
			}
		})
	}
}

func TestChangedPackagesModuleBelowRepositoryRoot(t *testing.T) {
	t.Parallel()
	r := newRepo(t, "master")
	r.write("sub/go.mod", "module example.com/sub\n\ngo 1.27\n")
	r.write("sub/root.go", "package sub\n")
	r.write("sub/a/a.go", "package a\n")
	r.write("other/o.go", "package other\n")
	base := r.commit("base")
	r.write("sub/root.go", "package sub\n\n// edited\n")
	r.write("sub/a/new.go", "package a\n")
	r.write("other/o.go", "package other\n\n// edited\n")

	got, err := ChangedPackages(context.Background(), filepath.Join(r.dir, "sub"), base, golang.New(), Head{})
	if err != nil {
		t.Fatalf("ChangedPackages: %v", err)
	}
	if want := []string{".", "a"}; !slices.Equal(got.Packages, want) {
		t.Errorf("Packages = %q, want %q", got.Packages, want)
	}
	if len(got.Deleted) != 0 {
		t.Errorf("Deleted = %q, want none", got.Deleted)
	}
}

func TestChangedPackagesErrors(t *testing.T) {
	t.Parallel()
	r, _ := newChangeRepo(t)
	for _, base := range []string{"", "--output=x", "0000000000000000000000000000000000000000"} {
		if _, err := ChangedPackages(context.Background(), r.dir, base, golang.New(), Head{}); err == nil {
			t.Errorf("ChangedPackages(%q): want error", base)
		}
	}
	if _, err := os.Stat(filepath.Join(r.dir, "x")); err == nil {
		t.Error("an option-like merge-base reached git")
	}
	if _, err := ChangedPackages(context.Background(), t.TempDir(), "HEAD", golang.New(), Head{}); err == nil {
		t.Error("ChangedPackages outside a repository: want error")
	}
}

func TestChangedPackagesTypeScriptBelowRepositoryRoot(t *testing.T) {
	t.Parallel()
	r := newRepo(t, "master")
	r.write("web/package.json", "{}\n")
	r.write("web/tsconfig.json", "{}\n")
	r.write("web/root.ts", "export const r = 1;\n")
	r.write("web/a/a.ts", "export const a = 1;\n")
	r.write("tsconfig.json", "{}\n")
	r.write("other/o.ts", "export const o = 1;\n")
	base := r.commit("base")
	r.write("web/a/__tests__/a.test.ts", "it(\"a\", () => {});\n")
	r.write("tsconfig.json", "{\"compilerOptions\": {}}\n")
	r.write("other/o.ts", "export const o = 2;\n")

	got, err := ChangedPackages(context.Background(), filepath.Join(r.dir, "web"), base, typescript.New(), Head{})
	if err != nil {
		t.Fatalf("ChangedPackages: %v", err)
	}
	if want := []string{"a"}; !slices.Equal(got.Packages, want) || got.All || len(got.Deleted) != 0 {
		t.Errorf("Change = %+v, want Packages %q only; a tsconfig outside the module moves nothing", got, want)
	}

	r.write("web/tsconfig.json", "{\"compilerOptions\": {}}\n")
	got, err = ChangedPackages(context.Background(), filepath.Join(r.dir, "web"), base, typescript.New(), Head{})
	if err != nil {
		t.Fatalf("ChangedPackages: %v", err)
	}
	if !got.All {
		t.Errorf("Change = %+v, want All after the module's tsconfig.json changed", got)
	}
}

// newTSChangeRepo commits a TypeScript module with packages a (with a
// __tests__ directory and a declaration file) and b, a tsconfig extending
// a file under config, a dependency under node_modules, a build output and
// a nested module on master, and returns the repository and the commit
// hash.
func newTSChangeRepo(t *testing.T) (*testRepo, string) {
	t.Helper()
	r := newRepo(t, "master")
	r.write("package.json", "{}\n")
	r.write("tsconfig.json", "{\"extends\": \"./config/tsconfig.base.json\"}\n")
	r.write("config/tsconfig.base.json", "{}\n")
	r.write("README.md", "readme\n")
	r.write("a/a.ts", "export const a = 1;\n")
	r.write("a/a.test.ts", "it(\"a\", () => {});\n")
	r.write("a/__tests__/deep/a.test.ts", "it(\"a\", () => {});\n")
	r.write("a/types.d.ts", "export type A = number;\n")
	r.write("b/b.ts", "export const b = 1;\n")
	r.write("b/b.js", "exports.b = 1;\n")
	r.write("node_modules/dep/index.ts", "export const d = 1;\n")
	r.write("dist/out.ts", "export const o = 1;\n")
	r.write("tools/package.json", "{}\n")
	r.write("tools/gen.ts", "export const g = 1;\n")
	return r, r.commit("base")
}

func TestChangedPackagesTypeScript(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		change      func(r *testRepo)
		wantPkgs    []string
		wantDeleted []string
		wantAll     bool
	}{
		{
			name:   "no change",
			change: func(*testRepo) {},
		},
		{
			name:     "source file",
			change:   func(r *testRepo) { r.write("b/b.ts", "export const b = 2;\n") },
			wantPkgs: []string{"b"},
		},
		{
			name:     "test file beside the source",
			change:   func(r *testRepo) { r.write("a/a.test.ts", "it(\"b\", () => {});\n") },
			wantPkgs: []string{"a"},
		},
		{
			name:     "test file under __tests__ belongs to the enclosing package",
			change:   func(r *testRepo) { r.write("a/__tests__/deep/a.test.ts", "it(\"b\", () => {});\n") },
			wantPkgs: []string{"a"},
		},
		{
			name:     "declaration file",
			change:   func(r *testRepo) { r.write("a/types.d.ts", "export type A = string;\n") },
			wantPkgs: []string{"a"},
		},
		{
			name:   "test file in a directory with no package",
			change: func(r *testRepo) { r.write("e2e/x.spec.ts", "it(\"x\", () => {});\n") },
		},
		{
			name: "non-TypeScript files and skipped directories",
			change: func(r *testRepo) {
				r.write("README.md", "changed\n")
				r.write("b/b.js", "exports.b = 2;\n")
				r.write("node_modules/dep/index.ts", "export const d = 2;\n")
				r.write("dist/out.ts", "export const o = 2;\n")
				r.write(".cache/c.ts", "export const c = 1;\n")
			},
		},
		{
			name: "nested module",
			change: func(r *testRepo) {
				r.write("tools/gen.ts", "export const g = 2;\n")
				r.write("tools/package.json", "{\"name\": \"tools\"}\n")
				r.write("tools/tsconfig.json", "{}\n")
			},
		},
		{
			name: "every source file of a package deleted",
			change: func(r *testRepo) {
				r.git("rm", "-q", "b/b.ts")
				r.commit("drop b")
			},
			wantDeleted: []string{"b"},
		},
		{
			name:    "root tsconfig selects every package",
			change:  func(r *testRepo) { r.write("tsconfig.json", "{\"compilerOptions\": {}}\n") },
			wantAll: true,
		},
		{
			name:    "extended tsconfig selects every package",
			change:  func(r *testRepo) { r.write("config/tsconfig.base.json", "{\"compilerOptions\": {}}\n") },
			wantAll: true,
		},
		{
			name: "root package.json selects every package",
			change: func(r *testRepo) {
				r.write("package.json", "{\"name\": \"m\"}\n")
				r.write("b/b.ts", "export const b = 2;\n")
			},
			wantPkgs: []string{"b"},
			wantAll:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, base := newTSChangeRepo(t)
			tt.change(r)
			got, err := ChangedPackages(context.Background(), r.dir, base, typescript.New(), Head{})
			if err != nil {
				t.Fatalf("ChangedPackages: %v", err)
			}
			if !slices.Equal(got.Packages, tt.wantPkgs) {
				t.Errorf("Packages = %q, want %q", got.Packages, tt.wantPkgs)
			}
			if !slices.Equal(got.Deleted, tt.wantDeleted) {
				t.Errorf("Deleted = %q, want %q", got.Deleted, tt.wantDeleted)
			}
			if got.All != tt.wantAll {
				t.Errorf("All = %v, want %v", got.All, tt.wantAll)
			}
		})
	}
}
