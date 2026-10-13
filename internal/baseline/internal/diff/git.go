// Package diff holds the git primitives package baseline composes: running
// git, locating a module in its repository, the index a staged check reads,
// and the two listings of what changed since a merge-base, the changed
// packages (SPEC.md 8.4) and the renames a moved package is seeded from.
package diff

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// overrides are environment variables, set when astimate runs inside a git
// hook, that would redirect git away from the repository at cmd.Dir.
func overrides() []string {
	return []string{"GIT_DIR=", "GIT_WORK_TREE=", "GIT_INDEX_FILE="}
}

// noHooks are the arguments every git child process gets: hooks are off, so
// a baseline checkout or a staged copy, which is read-only scaffolding, runs
// none of the repository's hooks (a post-checkout hook that needs git-lfs,
// say).
func noHooks() []string {
	return []string{"-c", "core.hooksPath=" + os.DevNull}
}

// Git runs git with args as separate arguments (never through a shell) in
// dir and returns its trimmed standard output. It runs no hooks. The error
// includes git's standard error and wraps the *exec.ExitError.
func Git(ctx context.Context, dir string, args ...string) (string, error) {
	return GitEnv(ctx, dir, nil, args...)
}

// GitEnv is Git with the entries of env added to the environment after the
// variables a git hook sets to redirect git are dropped (WithoutOverrides),
// such as a GIT_INDEX_FILE that selects the index to read.
func GitEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append(noHooks(), args...)...)
	cmd.Dir = dir
	cmd.Env = append(WithoutOverrides(os.Environ()), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", args[0], err)
		}
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, msg)
	}
	return strings.TrimSpace(string(out)), nil
}

// WithoutOverrides returns env without GIT_DIR, GIT_WORK_TREE and
// GIT_INDEX_FILE, which a git hook sets and which would redirect git away
// from the repository it runs in.
func WithoutOverrides(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !slices.ContainsFunc(overrides(), func(prefix string) bool { return strings.HasPrefix(kv, prefix) }) {
			out = append(out, kv)
		}
	}
	return out
}

// RepoRelative returns root's path relative to the top of its git working
// tree, so the same module can be found inside a worktree of that
// repository.
func RepoRelative(ctx context.Context, root string) (string, error) {
	top, err := Git(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("finding repository root of %s: %w", root, err)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", root, err)
	}
	// git reports the top level with symlinks resolved; resolve root too so
	// the two paths are comparable (for example /var and /private/var).
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	if resolved, err := filepath.EvalSymlinks(top); err == nil {
		top = resolved
	}
	rel, err := filepath.Rel(top, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("locating %s within repository %s: not inside it", root, top)
	}
	return rel, nil
}

// IndexEnv returns the environment entries that point git at indexFile,
// made absolute against the repository root top; none when indexFile is
// empty.
func IndexEnv(top, indexFile string) ([]string, error) {
	if indexFile == "" {
		return nil, nil
	}
	if !filepath.IsAbs(indexFile) {
		indexFile = filepath.Join(top, indexFile)
	}
	if _, err := os.Stat(indexFile); err != nil {
		return nil, fmt.Errorf("reading index file: %w", err)
	}
	return []string{"GIT_INDEX_FILE=" + indexFile}, nil
}
