package baseline

import (
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

	"github.com/rfizzle/astimate/internal/baseline/internal/diff"
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

// ModulePathFunc returns the module path of the module rooted at root,
// which package identifiers are relative to (PackageDir); "" for a
// language whose identifiers already are module-relative directories.
type ModulePathFunc func(root string) (string, error)

// FromGit returns the baseline at the merge-base of HEAD and ref in the git
// repository containing root. An empty ref means DefaultRef. The merge-base
// is checked out into a temporary detached worktree, every package ext lists
// there is extracted with a ModuleContext carrying the module path
// modulePath reads there (none when modulePath is nil), and the worktree is
// removed before FromGit returns, including on error, panic, cancellation
// of ctx, and SIGINT or SIGTERM received meanwhile. root may be a module
// nested below the repository root. Rows are keyed by the directory each
// package has relative to that module path (PackageDir), so a module path
// changed since the merge-base still matches every package. Ref reports
// the merge-base commit and Tokenizer reports tokenizer, the method ext
// counts tokens with.
// When ext implements metrics.FunctionLister, each Row carries the
// package's functions at the merge-base (CollectFunctions); when it
// implements metrics.ModuleMetrics and metrics.ModuleDetailer, CrossBlocks
// reports the module's cross-package blocks there (CollectCrossBlocks);
// when it implements metrics.ImporterLister, Importers reports each
// package's importers there (CollectImporters).
// An extraction error names paths relative to the module root
// (TreeRelative), not the temporary worktree.
// When ext implements metrics.Forgetter, FromGit calls Forget on the
// worktree's module root before removing it, so ext keeps nothing loaded
// from the baseline.
func FromGit(ctx context.Context, root, ref string, ext metrics.Extractor, modulePath ModulePathFunc, tokenizer string) (Baseline, error) {
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
	rel, err := diff.RepoRelative(ctx, root)
	if err != nil {
		return nil, err
	}

	wt, cleanup, err := addWorktree(ctx, root, sha)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	modRoot := filepath.Join(wt, rel)
	baseModPath := ""
	if modulePath != nil {
		if baseModPath, err = modulePath(modRoot); err != nil {
			return nil, fmt.Errorf("reading the module path at %s: %w", sha, TreeRelative(err, modRoot))
		}
	}
	mod := &metrics.ModuleContext{Root: modRoot, ModulePath: baseModPath}
	pkgs, err := Collect(ctx, ext, mod)
	var funcs map[string][]metrics.FunctionInfo
	if err == nil {
		funcs, err = CollectFunctions(ctx, ext, mod, pkgs)
	}
	var cross []metrics.CrossBlock
	crossKnown := false
	if _, isModule := ext.(metrics.ModuleMetrics); err == nil && isModule {
		cross, crossKnown, err = CollectCrossBlocks(ctx, ext, mod)
	}
	var importers map[string][]string
	importersKnown := false
	if err == nil {
		importers, importersKnown, err = CollectImporters(ctx, ext, mod, pkgs)
	}
	// The worktree is deleted on return, so nothing will ask for its load
	// again; let an extractor that caches loads release it.
	if f, ok := ext.(metrics.Forgetter); ok {
		f.Forget(modRoot)
	}
	if err != nil {
		return nil, fmt.Errorf("extracting baseline at %s: %w", sha, TreeRelative(err, modRoot))
	}
	dir := func(id string) string { return PackageDir(baseModPath, id) }
	c := collected{pkgs: pkgs, funcs: funcs, dir: dir}
	byDir := make(map[string][]string, len(importers))
	for id, from := range importers {
		for _, imp := range from {
			byDir[dir(id)] = append(byDir[dir(id)], dir(imp))
		}
	}
	return &snapshot{
		ref: sha, tokenizer: tokenizer, rows: c.rows(), cross: metrics.MapPackages(cross, dir), crossKnown: crossKnown,
		importers: byDir, importersKnown: importersKnown,
	}, nil
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
	base, err := diff.Git(ctx, root, "merge-base", "HEAD", sha)
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
	out, err := diff.Git(ctx, root, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && ctx.Err() == nil {
			return "", false, nil
		}
		return "", false, fmt.Errorf("resolving ref %q: %w", ref, err)
	}
	return out, true, nil
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
		if _, err := diff.Git(cctx, root, "worktree", "remove", "--force", dir); err == nil {
			_ = os.RemoveAll(dir)
			return
		}
		// Fallback: delete the directory ourselves and let git forget the
		// now-missing worktree.
		_ = os.RemoveAll(dir)
		_, _ = diff.Git(cctx, root, "worktree", "prune")
	}
	if _, err := diff.Git(ctx, root, "worktree", "add", "--detach", dir, sha); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("checking out baseline %s: %w", sha, err)
	}
	return dir, cleanup, nil
}
