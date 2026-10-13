package diff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang"
)

// testGit runs git in dir with user and system config ignored and a fixed
// identity, failing t on error.
func testGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	env := []string{
		"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	}
	if _, err := GitEnv(context.Background(), dir, env, args...); err != nil {
		t.Fatalf("git %q: %v", args, err)
	}
}

// writeFile writes body at rel below dir, creating its directory.
func writeFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// moverSource is a Go file of package name long enough that a rename that
// changes only its package clause is detected as one.
func moverSource(name string) string {
	body := "package " + name + "\n\n"
	for _, f := range []string{"One", "Two", "Three", "Four", "Five", "Six"} {
		body += "// " + f + " returns its name.\nfunc " + f + "() string {\n\treturn \"" + f + "\"\n}\n\n"
	}
	return body
}

// TestModuleBelowRoot moves a file between packages of a module nested in
// its repository's svc directory and checks every primitive against it:
// the module's place in the repository, the changed packages, the renames
// against the working tree and the index, and the index a hook names.
func TestModuleBelowRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Parallel()
	ctx := context.Background()

	repo := t.TempDir()
	root := filepath.Join(repo, "svc")
	testGit(t, repo, "init", "-q", "-b", "master")
	writeFile(t, root, "go.mod", "module example.com/svc\n")
	writeFile(t, root, "a/x.go", moverSource("a"))
	writeFile(t, root, "a/a_test.go", moverSource("a"))
	writeFile(t, repo, "other/y.go", moverSource("other"))
	testGit(t, repo, "add", "-A")
	testGit(t, repo, "commit", "-q", "-m", "base")
	base, err := Git(ctx, repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"a/b", "t", "c"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	testGit(t, repo, "mv", "svc/a/x.go", "svc/a/b/x.go")
	testGit(t, repo, "mv", "svc/a/a_test.go", "svc/t/a_test.go")
	testGit(t, repo, "mv", "other/y.go", "svc/c/y.go")

	if rel, err := RepoRelative(ctx, root); err != nil || rel != "svc" {
		t.Errorf("RepoRelative = %q, %v, want svc", rel, err)
	}
	change, err := ChangedPackages(ctx, root, base, golang.New(), Head{})
	if err != nil {
		t.Fatalf("ChangedPackages: %v", err)
	}
	if want := []string{"a/b", "c", "t"}; !slices.Equal(change.Packages, want) {
		t.Errorf("ChangedPackages = %q, want %q", change.Packages, want)
	}
	// Only the non-test file moved within the module is a rename: the test
	// file does not count and other/ lies outside the module.
	want := Renames{"a/b": {{Dir: "a", Files: 1}}}
	for _, head := range []Head{{}, {Staged: true}} {
		got, err := FindRenames(ctx, root, base, golang.New(), head)
		if err != nil {
			t.Fatalf("FindRenames(%+v): %v", head, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FindRenames(%+v) = %+v, want %+v", head, got, want)
		}
	}
	if _, err := FindRenames(ctx, root, "-x", golang.New(), Head{}); err == nil {
		t.Error("FindRenames accepted a merge-base starting with a dash")
	}

	if env, err := IndexEnv(repo, ""); err != nil || env != nil {
		t.Errorf("IndexEnv with no file = %q, %v, want none", env, err)
	}
	env, err := IndexEnv(repo, ".git/index")
	if want := "GIT_INDEX_FILE=" + filepath.Join(repo, ".git", "index"); err != nil || !slices.Equal(env, []string{want}) {
		t.Errorf("IndexEnv = %q, %v, want %q", env, err, want)
	}
	if _, err := IndexEnv(repo, "missing"); err == nil {
		t.Error("IndexEnv accepted a missing index file")
	}
}

func TestWithoutOverrides(t *testing.T) {
	t.Parallel()

	got := WithoutOverrides([]string{"HOME=/h", "GIT_DIR=/x", "GIT_WORK_TREE=/y", "GIT_INDEX_FILE=/z", "GIT_AUTHOR_NAME=a"})
	if want := []string{"HOME=/h", "GIT_AUTHOR_NAME=a"}; !slices.Equal(got, want) {
		t.Errorf("WithoutOverrides = %q, want %q", got, want)
	}
}
