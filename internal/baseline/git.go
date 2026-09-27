package baseline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rfizzle/astimate/internal/metrics"
)

// ErrNoDefaultRef is returned by DefaultRef when none of the default
// baseline refs exists.
var ErrNoDefaultRef = errors.New("no default baseline ref")

// defaultRefs are the refs DefaultRef tries, in order (SPEC.md 8.3).
func defaultRefs() []string {
	return []string{"origin/master", "master", "origin/main", "main"}
}

// cleanupTimeout bounds the git commands that remove a baseline worktree.
// They run on a context detached from the caller's, which may already be
// cancelled.
const cleanupTimeout = 30 * time.Second

// FromGit returns the baseline at the merge-base of HEAD and ref in the git
// repository containing root. An empty ref means DefaultRef. The merge-base
// is checked out into a temporary detached worktree, every package ext lists
// there is extracted with a ModuleContext carrying modulePath, and the
// worktree is removed before FromGit returns, including on error, panic,
// cancellation of ctx, and SIGINT or SIGTERM received meanwhile. root may be
// a module nested below the repository root. Ref reports the merge-base
// commit and Tokenizer reports tokenizer, the method ext counts tokens with.
func FromGit(ctx context.Context, root, ref string, ext metrics.Extractor, modulePath, tokenizer string) (Baseline, error) {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if ref == "" {
		r, err := DefaultRef(ctx, root)
		if err != nil {
			return nil, err
		}
		ref = r
	}
	sha, err := MergeBase(ctx, root, ref)
	if err != nil {
		return nil, err
	}
	rel, err := repoRelative(ctx, root)
	if err != nil {
		return nil, err
	}

	wt, cleanup, err := addWorktree(ctx, root, sha)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	pkgs, err := Collect(ctx, ext, &metrics.ModuleContext{
		Root:       filepath.Join(wt, rel),
		ModulePath: modulePath,
	})
	if err != nil {
		return nil, fmt.Errorf("extracting baseline at %s: %w", sha, err)
	}
	return &snapshot{ref: sha, tokenizer: tokenizer, pkgs: pkgs}, nil
}

// DefaultRef returns the first of origin/master, master, origin/main and main
// that names a commit in the repository containing root. When none does it
// returns an error wrapping ErrNoDefaultRef that lists all four.
func DefaultRef(ctx context.Context, root string) (string, error) {
	refs := defaultRefs()
	for _, ref := range refs {
		_, found, err := resolveCommit(ctx, root, ref)
		if err != nil {
			return "", err
		}
		if found {
			return ref, nil
		}
	}
	return "", fmt.Errorf("%w: tried %s", ErrNoDefaultRef, strings.Join(refs, ", "))
}

// MergeBase returns the commit hash of the merge-base of HEAD and ref in the
// repository containing root.
func MergeBase(ctx context.Context, root, ref string) (string, error) {
	sha, found, err := resolveCommit(ctx, root, ref)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("resolving baseline ref %q: not a commit", ref)
	}
	base, err := git(ctx, root, "merge-base", "HEAD", sha)
	if err != nil {
		return "", fmt.Errorf("finding merge-base of HEAD and %s: %w", ref, err)
	}
	return base, nil
}

// HeadCommit returns the commit hash HEAD names in the repository containing
// root.
func HeadCommit(ctx context.Context, root string) (string, error) {
	sha, found, err := resolveCommit(ctx, root, "HEAD")
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("resolving HEAD: no commit")
	}
	return sha, nil
}

// resolveCommit resolves ref to a commit hash. found is false, with a nil
// error, when ref does not name a commit; err reports any other git failure,
// such as root not being in a repository.
func resolveCommit(ctx context.Context, root, ref string) (sha string, found bool, err error) {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return "", false, fmt.Errorf("resolving ref %q: not a valid ref", ref)
	}
	// The leading-dash check above keeps ref from being read as an option.
	out, err := git(ctx, root, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && ctx.Err() == nil {
			return "", false, nil
		}
		return "", false, fmt.Errorf("resolving ref %q: %w", ref, err)
	}
	return out, true, nil
}

// repoRelative returns root's path relative to the top of its git working
// tree, so the same module can be found inside a worktree of that
// repository.
func repoRelative(ctx context.Context, root string) (string, error) {
	top, err := git(ctx, root, "rev-parse", "--show-toplevel")
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

// addWorktree checks sha out into a new temporary directory as a detached
// worktree of the repository containing root. The returned cleanup removes
// the worktree and its directory; it is safe to call on any exit path,
// including after ctx is cancelled, and must be called exactly once.
func addWorktree(ctx context.Context, root, sha string) (dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp("", "astimate-baseline-")
	if err != nil {
		return "", nil, fmt.Errorf("creating baseline worktree directory: %w", err)
	}
	cleanup = func() {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		if _, err := git(cctx, root, "worktree", "remove", "--force", dir); err == nil {
			_ = os.RemoveAll(dir)
			return
		}
		// Fallback: delete the directory ourselves and let git forget the
		// now-missing worktree.
		_ = os.RemoveAll(dir)
		_, _ = git(cctx, root, "worktree", "prune")
	}
	if _, err := git(ctx, root, "worktree", "add", "--detach", dir, sha); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("checking out baseline %s: %w", sha, err)
	}
	return dir, cleanup, nil
}

// gitEnvOverrides are environment variables, set when astimate runs inside a
// git hook, that would redirect git away from the repository at cmd.Dir.
func gitEnvOverrides() []string {
	return []string{"GIT_DIR=", "GIT_WORK_TREE=", "GIT_INDEX_FILE="}
}

// git runs git with args as separate arguments (never through a shell) in
// dir and returns its trimmed standard output. The error includes git's
// standard error and wraps the *exec.ExitError.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = withoutGitOverrides(os.Environ())
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

// withoutGitOverrides returns env without the variables gitEnvOverrides
// names.
func withoutGitOverrides(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		drop := false
		for _, prefix := range gitEnvOverrides() {
			if strings.HasPrefix(kv, prefix) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}
