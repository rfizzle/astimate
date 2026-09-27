package baseline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNoRepository is returned by StagedTree when root is not inside a git
// working tree, so there is no index to check.
var ErrNoRepository = errors.New("not in a git repository")

// StagedTree copies the files the git index holds into a new temporary
// directory and returns root's counterpart in that copy with a cleanup that
// removes it. root may be a module nested below the repository root; the
// whole repository is copied, so paths such as a replace directive's
// ../other still resolve. indexFile is the index to read, as
// GIT_INDEX_FILE names it inside a git hook (`git commit -a` and `git
// commit <paths>` commit a temporary index); empty means the repository's
// own index. A relative indexFile is taken relative to the repository root.
// Unstaged changes and untracked files are absent from the copy, and a
// staged deletion is honoured. The copy is not a git repository: git
// commands keep running against root. cleanup is safe to call on any exit
// path and must be called exactly once. It returns an error wrapping
// ErrNoRepository when root is not in a git working tree.
func StagedTree(ctx context.Context, root, indexFile string) (tree string, cleanup func(), err error) {
	top, err := git(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", nil, fmt.Errorf("finding repository root of %s: %w: %w", root, ErrNoRepository, err)
	}
	rel, err := repoRelative(ctx, root)
	if err != nil {
		return "", nil, err
	}
	env, err := indexEnv(top, indexFile)
	if err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp("", "astimate-staged-")
	if err != nil {
		return "", nil, fmt.Errorf("creating staged tree directory: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	// --prefix must end in a separator to name a directory; git creates the
	// directories below it. Running at the top level makes -a cover the
	// whole index rather than the part below the working directory.
	if _, err := gitEnv(ctx, top, env, "checkout-index", "--all", "--prefix="+dir+string(filepath.Separator)); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("checking out the index into %s: %w", dir, err)
	}
	return filepath.Join(dir, rel), cleanup, nil
}

// indexEnv returns the environment entries that point git at indexFile,
// made absolute against the repository root top; none when indexFile is
// empty.
func indexEnv(top, indexFile string) ([]string, error) {
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
