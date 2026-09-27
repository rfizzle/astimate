package baseline

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

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
			got, err := ChangedPackages(context.Background(), r.dir, base)
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

	got, err := ChangedPackages(context.Background(), filepath.Join(r.dir, "sub"), base)
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
		if _, err := ChangedPackages(context.Background(), r.dir, base); err == nil {
			t.Errorf("ChangedPackages(%q): want error", base)
		}
	}
	if _, err := os.Stat(filepath.Join(r.dir, "x")); err == nil {
		t.Error("an option-like merge-base reached git")
	}
	if _, err := ChangedPackages(context.Background(), t.TempDir(), "HEAD"); err == nil {
		t.Error("ChangedPackages outside a repository: want error")
	}
}
