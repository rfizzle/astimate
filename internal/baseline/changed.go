package baseline

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/rfizzle/astimate/internal/metrics"
)

// Change is the set of packages a change touches relative to a merge-base
// (SPEC.md 8.4).
type Change struct {
	// Packages are the module-relative, slash-separated package directories
	// ("." for the module root, matching assess's package_path) that a
	// changed source file belongs to, test files included, and that still
	// hold a file making them a package in the head tree. Sorted.
	Packages []string
	// Deleted are the module-relative directories that a changed file
	// making them a package belongs to but that hold no such file in the
	// head tree: packages removed or renamed away since the merge-base.
	// Callers report them in a summary line, never as violations. Sorted.
	Deleted []string
	// All reports that a changed file can move every package's metrics,
	// such as configuration that import resolution reads; callers then
	// check every package. Packages and Deleted are filled regardless.
	All bool
}

// Head is the head side ChangedPackages compares with a merge-base: the
// working tree when zero, or the index.
type Head struct {
	// Staged compares the index (git diff --cached) instead of the working
	// tree: unstaged changes and untracked files do not count.
	Staged bool
	// IndexFile is the index Staged reads, as StagedTree takes it; empty
	// means the repository's own index. Ignored unless Staged.
	IndexFile string
	// Tree is root's counterpart in a copy of the head tree, such as
	// StagedTree returns, whose files decide which changed directories
	// still hold a package and which lie in a nested module; empty means
	// root itself.
	Tree string
}

// ChangedPackages returns the packages of the module at root whose source
// files differ between the commit mergeBase and head, with sc saying which
// files are source and which package each belongs to. Against the working
// tree, committed, staged, unstaged and untracked (but not ignored) files
// all count; against the index, committed and staged files do. Files sc
// classifies as metrics.NotSource, files outside root and files in a
// module nested below root are ignored; a directory is in a nested module
// when it or an ancestor below root holds a file sc reports as a module
// marker in head's tree.
//
// Selecting every package (--all) is the caller's concern: it checks every
// package the extractor lists and does not call ChangedPackages.
func ChangedPackages(ctx context.Context, root, mergeBase string, sc metrics.SourceClassifier, head Head) (Change, error) {
	if mergeBase == "" || strings.HasPrefix(mergeBase, "-") {
		return Change{}, fmt.Errorf("listing changes since %q: not a valid commit", mergeBase)
	}
	rel, err := repoRelative(ctx, root)
	if err != nil {
		return Change{}, err
	}
	paths, err := changedPaths(ctx, root, mergeBase, head)
	if err != nil {
		return Change{}, err
	}
	if head.Tree != "" {
		root = head.Tree
	}

	var c Change
	nested := make(map[string]bool)
	files := sourceFiles(paths, filepath.ToSlash(rel), sc)
	for _, f := range files.module {
		in, err := inNestedModule(root, path.Dir(f), sc, nested)
		if err != nil {
			return Change{}, err
		}
		if !in {
			c.All = true
			break
		}
	}
	isPkg := make(map[string]bool, len(files.dirs))
	for _, d := range files.dirs {
		in, err := inNestedModule(root, d.dir, sc, nested)
		if err != nil {
			return Change{}, err
		}
		if in {
			continue
		}
		has, ok := isPkg[d.pkg]
		if !ok {
			has, err = hasPackageFiles(root, d.pkg, sc)
			if err != nil {
				return Change{}, err
			}
			isPkg[d.pkg] = has
		}
		switch {
		case has:
			c.Packages = append(c.Packages, d.pkg)
		case d.defines:
			c.Deleted = append(c.Deleted, d.pkg)
		}
	}
	c.Packages = slices.Compact(c.Packages)
	c.Deleted = slices.Compact(c.Deleted)
	return c, nil
}

// changedPaths returns the paths, relative to the top level of the
// repository containing root, that differ between mergeBase and head:
// against the working tree the diff plus the untracked, non-ignored files;
// against the index the cached diff alone.
func changedPaths(ctx context.Context, root, mergeBase string, head Head) ([]string, error) {
	// --no-renames lists a rename as its deleted and its added path, so the
	// directories on both sides are considered. --no-relative keeps paths
	// relative to the repository top level whatever diff.relative says.
	args := []string{"diff", "--name-only", "-z", "--no-renames", "--no-relative", mergeBase, "--"}
	var env []string
	if head.Staged {
		top, err := git(ctx, root, "rev-parse", "--show-toplevel")
		if err != nil {
			return nil, fmt.Errorf("finding repository root of %s: %w", root, err)
		}
		if env, err = indexEnv(top, head.IndexFile); err != nil {
			return nil, err
		}
		args = slices.Insert(args, 1, "--cached")
	}
	diff, err := gitEnv(ctx, root, env, args...)
	if err != nil {
		return nil, fmt.Errorf("listing files changed since %s: %w", mergeBase, err)
	}
	paths := splitNUL(diff)
	if head.Staged {
		return paths, nil
	}
	untracked, err := git(ctx, root, "ls-files", "-z", "--others", "--exclude-standard", "--full-name")
	if err != nil {
		return nil, fmt.Errorf("listing untracked files: %w", err)
	}
	return append(paths, splitNUL(untracked)...), nil
}

// splitNUL splits NUL-separated git output into its non-empty entries.
func splitNUL(out string) []string {
	var entries []string
	for e := range strings.SplitSeq(out, "\x00") {
		if e != "" {
			entries = append(entries, e)
		}
	}
	return entries
}

// changedDir is a package that changed source files belong to, and the
// directory they are in.
type changedDir struct {
	// pkg is the module-relative package directory.
	pkg string
	// dir is the module-relative directory of the changed files: pkg, or
	// one below it such as a TypeScript __tests__ directory.
	dir string
	// defines reports that one of the files makes pkg a package
	// (metrics.PackageSource), so pkg is deleted when it holds none.
	defines bool
}

// changedFiles are a module's changed source files, grouped.
type changedFiles struct {
	// dirs are the packages the files belong to, one entry per package
	// and directory, sorted by pkg and then dir.
	dirs []changedDir
	// module are the module-relative paths of the files that can move
	// every package (metrics.ModuleSource).
	module []string
}

// sourceFiles makes paths, slash-separated and relative to the repository
// top level, relative to the module at prefix ("." when the module is the
// repository root) and groups them as sc classifies them. Files outside
// the module or that sc says are not source are dropped. It does not touch
// the filesystem.
func sourceFiles(paths []string, prefix string, sc metrics.SourceClassifier) changedFiles {
	prefix = path.Clean(prefix)
	index := make(map[changedDir]int, len(paths))
	var out changedFiles
	for _, p := range paths {
		p = path.Clean(p)
		if prefix != "." {
			rest, ok := strings.CutPrefix(p, prefix+"/")
			if !ok {
				continue
			}
			p = rest
		}
		sf := sc.ClassifyFile(p)
		switch sf.Kind {
		case metrics.ModuleSource:
			out.module = append(out.module, p)
			continue
		case metrics.PackageSource, metrics.MemberSource:
		default:
			continue
		}
		key := changedDir{pkg: sf.Package, dir: path.Dir(p)}
		i, ok := index[key]
		if !ok {
			i = len(out.dirs)
			index[key] = i
			out.dirs = append(out.dirs, key)
		}
		out.dirs[i].defines = out.dirs[i].defines || sf.Kind == metrics.PackageSource
	}
	slices.SortFunc(out.dirs, func(a, b changedDir) int {
		if c := strings.Compare(a.pkg, b.pkg); c != 0 {
			return c
		}
		return strings.Compare(a.dir, b.dir)
	})
	return out
}

// inNestedModule reports whether the module-relative, slash-separated dir
// lies in a module nested below root: whether dir or one of its ancestors
// below root holds a regular file sc reports as a module marker in the
// working tree. Answers are memoized in seen by directory. It returns an
// error when a directory on the path exists but cannot be read.
func inNestedModule(root, dir string, sc metrics.SourceClassifier, seen map[string]bool) (bool, error) {
	if dir == "." {
		return false, nil
	}
	if v, ok := seen[dir]; ok {
		return v, nil
	}
	v, err := hasMarker(filepath.Join(root, filepath.FromSlash(dir)), sc)
	if err != nil {
		return false, err
	}
	if !v {
		if v, err = inNestedModule(root, path.Dir(dir), sc, seen); err != nil {
			return false, err
		}
	}
	seen[dir] = v
	return v, nil
}

// hasMarker reports whether dir directly holds a regular file sc reports
// as a module marker. A directory that does not exist, or a path that is
// not a directory, holds none; any other read error is returned.
func hasMarker(dir string, sc metrics.SourceClassifier) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return false, nil
		}
		return false, fmt.Errorf("checking %s for a module marker: %w", dir, err)
	}
	for _, e := range entries {
		if e.Type().IsRegular() && sc.IsModuleMarker(e.Name()) {
			return true, nil
		}
	}
	return false, nil
}

// hasPackageFiles reports whether the module-relative directory pkg of the
// module at root directly holds a regular file that sc says makes pkg a
// package. A directory that no longer exists holds none.
func hasPackageFiles(root, pkg string, sc metrics.SourceClassifier) (bool, error) {
	dir := filepath.Join(root, filepath.FromSlash(pkg))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return false, nil
		}
		return false, fmt.Errorf("reading %s: %w", dir, err)
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if sf := sc.ClassifyFile(path.Join(pkg, e.Name())); sf.Kind == metrics.PackageSource && sf.Package == pkg {
			return true, nil
		}
	}
	return false, nil
}
