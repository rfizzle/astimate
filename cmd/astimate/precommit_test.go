package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// hookGitEnv returns the environment for git and hook commands in a
// temporary repository: the variables a surrounding git hook sets to
// redirect git dropped, user and system config ignored so the machine's
// identity, signing and hooks path do not apply, a fixed identity, and bin
// first on PATH.
func hookGitEnv(bin string) []string {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "GIT_DIR=") || strings.HasPrefix(kv, "GIT_WORK_TREE=") ||
			strings.HasPrefix(kv, "GIT_INDEX_FILE=") || strings.HasPrefix(kv, "PATH=")
	})
	return append(env,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
}

// gitRun runs git with env in dir and returns its standard output and
// standard error.
func gitRun(t *testing.T, dir string, env []string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = env
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

// gitCmd runs git with env in dir and fails the test if it fails.
func gitCmd(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	if out, errOut, err := gitRun(t, dir, env, args...); err != nil {
		t.Fatalf("git %s: %v\n%s%s", strings.Join(args, " "), err, out, errOut)
	}
}

// newFixtureRepo returns a new git repository, with no commit, on branch
// master, holding the pristine fixture under fixture/ and the module its
// replace directive points at under extmod/, so a baseline worktree of the
// repository resolves ../extmod too.
func newFixtureRepo(t *testing.T, env []string) string {
	t.Helper()
	repo := t.TempDir()
	for _, name := range []string{"fixture", "extmod"} {
		if err := os.CopyFS(filepath.Join(repo, name), os.DirFS(hookTestdata(name))); err != nil {
			t.Fatalf("copying %s: %v", name, err)
		}
	}
	gitCmd(t, repo, env, "init", "-q", "-b", "master")
	return repo
}

// degradedFiles are the files the degraded fixture adds to package tested.
func degradedFiles() []string {
	return []string{"degraded.go", "grade.go"}
}

// degradeFixture adds the degraded fixture's extra files to package tested
// in the repository repo.
func degradeFixture(t *testing.T, repo string) {
	t.Helper()
	for _, name := range degradedFiles() {
		data, err := os.ReadFile(filepath.Join(hookTestdata("fixture-degraded"), "tested", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, "fixture", "tested", name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestPreCommitSnippet installs the git hook script of docs/pre-commit.md in
// a temporary repository and checks that it skips the first commit, allows
// a commit that stages a harmless change beside an unstaged degradation,
// refuses the degradation once staged with the gate's exit code and
// violations on stderr, and allows a git commit -a that reverts it, which
// only the temporary index git hands the hook reflects.
func TestPreCommitSnippet(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: builds the binary and runs git")
	}
	requireTools(t, "git", "go", "sh")
	t.Parallel()

	script := docSnippet(t, "pre-commit.md", "pre-commit-snippet")
	env := append(hookGitEnv(buildAstimate(t)), "ASTIMATE_MODULE=fixture", "ASTIMATE_BASE=master")
	repo := newFixtureRepo(t, env)
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte(script), 0o700); err != nil { // a git hook must be executable
		t.Fatal(err)
	}

	commit := func(t *testing.T, msg string) (stderr string, err error) {
		t.Helper()
		gitCmd(t, repo, env, "add", "-A")
		_, stderr, err = gitRun(t, repo, env, "commit", "-q", "-m", msg)
		return stderr, err
	}

	stderr, err := commit(t, "pristine fixture")
	if err != nil {
		t.Fatalf("first commit refused: %v\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "skipping the gate for the first commit") {
		t.Errorf("first commit stderr = %q, want the skip notice", stderr)
	}

	// A harmless change to package tested, so each passing commit is gated
	// rather than empty.
	tested := filepath.Join(repo, "fixture", "tested", "tested.go")
	comment := func(t *testing.T) {
		t.Helper()
		src, err := os.ReadFile(tested)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tested, append(src, "\n// A comment changes no metric.\n"...), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// A partial stage: the harmless change is staged and the degradation
	// sits unstaged beside it in the same package. The hook judges only
	// what the commit records.
	degradeFixture(t, repo)
	comment(t)
	gitCmd(t, repo, env, "add", "fixture/tested/tested.go")
	if _, stderr, err := gitRun(t, repo, env, "commit", "-q", "-m", "comment tested"); err != nil {
		t.Fatalf("commit of the staged harmless change refused over an unstaged degradation: %v\n%s", err, stderr)
	}

	stderr, err = commit(t, "degrade tested")
	if err == nil {
		t.Fatalf("degrading commit succeeded, want it refused; stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "check exited 3") {
		t.Errorf("stderr does not report exit 3:\n%s", stderr)
	}
	for _, m := range hookDegradedMetrics() {
		if !strings.Contains(stderr, "    "+m+": ") {
			t.Errorf("stderr does not list the %s violation:\n%s", m, stderr)
		}
	}
	if strings.Contains(stderr, "astimate-staged-") {
		t.Errorf("stderr names the temporary staged tree, want module-relative paths:\n%s", stderr)
	}

	// Revert with git commit -a: the degradation is still in .git/index,
	// and only the temporary index git hands the hook in GIT_INDEX_FILE
	// has it removed.
	for _, name := range degradedFiles() {
		if err := os.Remove(filepath.Join(repo, "fixture", "tested", name)); err != nil {
			t.Fatal(err)
		}
	}
	comment(t)
	if _, stderr, err := gitRun(t, repo, env, "commit", "-q", "-a", "-m", "revert and comment tested"); err != nil {
		t.Fatalf("commit -a after revert refused: %v\n%s", err, stderr)
	}
	if out, _, err := gitRun(t, repo, env, "rev-list", "--count", "HEAD"); err != nil || strings.TrimSpace(out) != "3" {
		t.Errorf("rev-list --count HEAD = %q (%v), want 3 commits", out, err)
	}
}
