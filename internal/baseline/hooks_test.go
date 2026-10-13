package baseline

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestBaselineCheckoutRunsNoHooks checks that the baseline worktree and the
// staged copy are made without running the repository's hooks: a
// post-checkout hook that exits 1, as one that needs an absent git-lfs
// does, fails neither.
func TestBaselineCheckoutRunsNoHooks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hook is a shell script")
	}
	t.Parallel()

	r, first := twoCommitRepo(t)
	marker := filepath.Join(t.TempDir(), "ran")
	hook := "#!/bin/sh\ntouch '" + marker + "'\necho 'git-lfs: command not found' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(r.dir, ".git", "hooks", "post-checkout"), []byte(hook), 0o755); err != nil { //nolint:gosec // a hook must be executable
		t.Fatal(err)
	}

	t.Run("baseline worktree", func(t *testing.T) {
		ext := &stubExtractor{pkgs: []string{"a"}}
		if _, err := FromGit(t.Context(), r.dir, first, ext, nil, "est"); err != nil {
			t.Fatalf("FromGit with a failing post-checkout hook: %v", err)
		}
		assertCleanedUp(t, r, ext.root)
	})
	t.Run("staged copy", func(t *testing.T) {
		_, cleanup, err := StagedTree(t.Context(), r.dir, "")
		if err != nil {
			t.Fatalf("StagedTree with a failing post-checkout hook: %v", err)
		}
		cleanup()
	})
	if _, err := os.Stat(marker); err == nil {
		t.Error("the post-checkout hook ran")
	}
}
