package baseline

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang"
)

// readTree returns the slash-separated paths of the regular files under
// dir mapped to their contents.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	return files
}

// stagePartially stages an edit to a/a.go and then edits it again without
// staging, stages the removal of package b while keeping its files, and leaves
// c/c.go untracked.
func stagePartially(r *testRepo) {
	r.write("a/a.go", "package a\n\n// staged\n")
	r.git("add", "a/a.go")
	r.write("a/a.go", "package a\n\n// staged\n\n// unstaged\n")
	r.git("rm", "-q", "--cached", "b/b.go", "b/b_test.go")
	r.write("c/c.go", "package c\n")
}

func TestStagedTree(t *testing.T) {
	t.Parallel()
	r, _ := newChangeRepo(t)
	stagePartially(r)

	tree, cleanup, err := StagedTree(context.Background(), r.dir, "")
	if err != nil {
		t.Fatalf("StagedTree: %v", err)
	}
	files := readTree(t, tree)
	cleanup()
	if _, err := os.Stat(tree); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("staged tree %s still exists after cleanup: %v", tree, err)
	}

	if got, want := files["a/a.go"], "package a\n\n// staged\n"; got != want {
		t.Errorf("a/a.go = %q, want the staged content %q", got, want)
	}
	for _, name := range []string{"b/b.go", "b/b_test.go", "c/c.go"} {
		if _, ok := files[name]; ok {
			t.Errorf("%s is in the staged tree; want it absent (staged removal, untracked)", name)
		}
	}
	for _, name := range []string{"go.mod", "README.md", "nested/n/n.go"} {
		if _, ok := files[name]; !ok {
			t.Errorf("%s is missing from the staged tree", name)
		}
	}
}

func TestStagedTreeModuleBelowRepositoryRoot(t *testing.T) {
	t.Parallel()
	r := newRepo(t, "master")
	r.write("sub/go.mod", "module example.com/sub\n\ngo 1.27\n")
	r.write("other/o.go", "package other\n")
	r.commit("base")

	tree, cleanup, err := StagedTree(context.Background(), filepath.Join(r.dir, "sub"), "")
	if err != nil {
		t.Fatalf("StagedTree: %v", err)
	}
	defer cleanup()
	if filepath.Base(tree) != "sub" {
		t.Errorf("StagedTree = %s, want the counterpart of sub", tree)
	}
	for _, name := range []string{"go.mod", filepath.Join("..", "other", "o.go")} {
		if _, err := os.Stat(filepath.Join(tree, name)); err != nil {
			t.Errorf("%s missing from the staged tree: %v", name, err)
		}
	}
}

// TestStagedTreeIndexFile checks that StagedTree and ChangedPackages read
// the index they are given, as git commit -a and git commit <paths> hand a
// temporary one to their hooks, rather than the repository's own.
func TestStagedTreeIndexFile(t *testing.T) {
	t.Parallel()
	r, base := newChangeRepo(t)
	r.write("b/b.go", "package b\n\n// only in the other index\n")
	alt := filepath.Join(t.TempDir(), "index")
	data, err := os.ReadFile(filepath.Join(r.dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alt, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "b/b.go")
	cmd.Dir = r.dir
	cmd.Env = append(withoutGitOverrides(os.Environ()), "GIT_INDEX_FILE="+alt)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add with GIT_INDEX_FILE: %v\n%s", err, out)
	}

	for _, tt := range []struct {
		index string
		want  []string
	}{
		{index: "", want: nil},
		{index: alt, want: []string{"b"}},
	} {
		tree, cleanup, err := StagedTree(context.Background(), r.dir, tt.index)
		if err != nil {
			t.Fatalf("StagedTree(%q): %v", tt.index, err)
		}
		got, err := ChangedPackages(context.Background(), r.dir, base, golang.New(),
			Head{Staged: true, IndexFile: tt.index, Tree: tree})
		src, _ := os.ReadFile(filepath.Join(tree, "b", "b.go"))
		cleanup()
		if err != nil {
			t.Fatalf("ChangedPackages(index %q): %v", tt.index, err)
		}
		if !slices.Equal(got.Packages, tt.want) {
			t.Errorf("index %q: Packages = %q, want %q", tt.index, got.Packages, tt.want)
		}
		if edited := strings.Contains(string(src), "other index"); edited != (tt.index != "") {
			t.Errorf("index %q: staged b/b.go = %q", tt.index, src)
		}
	}
}

func TestStagedTreeErrors(t *testing.T) {
	t.Parallel()
	if _, _, err := StagedTree(context.Background(), t.TempDir(), ""); !errors.Is(err, ErrNoRepository) {
		t.Errorf("StagedTree outside a repository = %v, want ErrNoRepository", err)
	}
	r, _ := newChangeRepo(t)
	if _, _, err := StagedTree(context.Background(), r.dir, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("StagedTree with a missing index file: want error")
	}
}

func TestChangedPackagesStaged(t *testing.T) {
	t.Parallel()
	r, base := newChangeRepo(t)
	stagePartially(r)

	tree, cleanup, err := StagedTree(context.Background(), r.dir, "")
	if err != nil {
		t.Fatalf("StagedTree: %v", err)
	}
	defer cleanup()
	got, err := ChangedPackages(context.Background(), r.dir, base, golang.New(), Head{Staged: true, Tree: tree})
	if err != nil {
		t.Fatalf("ChangedPackages: %v", err)
	}
	// Package b's files are removed from the index but kept in the working
	// tree: the staged tree decides, so b is deleted there.
	if want := []string{"a"}; !slices.Equal(got.Packages, want) {
		t.Errorf("Packages = %q, want %q; the untracked c and unstaged edits do not count", got.Packages, want)
	}
	if want := []string{"b"}; !slices.Equal(got.Deleted, want) {
		t.Errorf("Deleted = %q, want %q", got.Deleted, want)
	}

	got, err = ChangedPackages(context.Background(), r.dir, base, golang.New(), Head{})
	if err != nil {
		t.Fatalf("ChangedPackages: %v", err)
	}
	if want := []string{"a", "b", "c"}; !slices.Equal(got.Packages, want) || len(got.Deleted) != 0 {
		t.Errorf("working tree Change = %+v, want Packages %q", got, want)
	}
}
