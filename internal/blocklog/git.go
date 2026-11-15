package blocklog

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// gitState returns the full hash of HEAD of the repository holding root
// and whether the working tree under root differs from it, untracked files
// included and the log at logPath left out, so the log never makes the
// tree read as dirty. Outside git, or before the first commit, it returns
// an empty hash and nil. Like every git command astimate starts, these run
// with no hooks (SPEC.md 8.4).
func gitState(ctx context.Context, root, logPath string) (string, *bool) {
	out, err := git(ctx, root, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		return "", nil
	}
	commit := strings.TrimSpace(string(out))
	args := []string{"status", "--porcelain", "--untracked-files=normal", "--", "."}
	if rel, err := filepath.Rel(root, logPath); err == nil && filepath.IsLocal(rel) {
		args = append(args, ":(exclude,literal)"+filepath.ToSlash(rel))
	}
	out, err = git(ctx, root, args...)
	if err != nil {
		return commit, nil
	}
	dirty := len(bytes.TrimSpace(out)) > 0
	return commit, &dirty
}

// git runs git with args in dir and returns its stdout. The variables a
// git hook sets to point git at another repository or index are dropped,
// so the state read is dir's.
func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Dir = dir
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return name == "GIT_DIR" || name == "GIT_WORK_TREE" || name == "GIT_INDEX_FILE"
	})
	return cmd.Output()
}
