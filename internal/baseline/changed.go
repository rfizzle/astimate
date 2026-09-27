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
)

// Change is the set of packages a change touches relative to a merge-base
// (SPEC.md 8.4).
type Change struct {
	// Packages are the module-relative, slash-separated package directories
	// ("." for the module root, matching assess's package_path) that have Go
	// files in the working tree and a changed Go file, test files included.
	// Sorted.
	Packages []string
	// Deleted are the module-relative directories that have a changed Go
	// file but no Go files left in the working tree: packages removed or
	// renamed away since the merge-base. Callers report them in a summary
	// line, never as violations. Sorted.
	Deleted []string
}

// ChangedPackages returns the packages of the module at root whose Go files
// differ between the commit mergeBase and the working tree. Committed,
// staged, unstaged and untracked (but not ignored) files all count. Non-Go
// files, files outside root, files in a module nested below root and files
// under a testdata directory are ignored.
//
// Selecting every package (--all) is the caller's concern: it checks every
// package the extractor lists and does not call ChangedPackages.
func ChangedPackages(ctx context.Context, root, mergeBase string) (Change, error) {
	if mergeBase == "" || strings.HasPrefix(mergeBase, "-") {
		return Change{}, fmt.Errorf("listing changes since %q: not a valid commit", mergeBase)
	}
	rel, err := repoRelative(ctx, root)
	if err != nil {
		return Change{}, err
	}
	// --no-renames lists a rename as its deleted and its added path, so the
	// directories on both sides are considered. --no-relative keeps paths
	// relative to the repository top level whatever diff.relative says.
	diff, err := git(ctx, root, "diff", "--name-only", "-z", "--no-renames", "--no-relative", mergeBase, "--")
	if err != nil {
		return Change{}, fmt.Errorf("listing files changed since %s: %w", mergeBase, err)
	}
	untracked, err := git(ctx, root, "ls-files", "-z", "--others", "--exclude-standard", "--full-name")
	if err != nil {
		return Change{}, fmt.Errorf("listing untracked files: %w", err)
	}
	paths := append(splitNUL(diff), splitNUL(untracked)...)

	var c Change
	for _, dir := range packageDirs(paths, filepath.ToSlash(rel)) {
		if inNestedModule(root, dir) {
			continue
		}
		has, err := hasGoFiles(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			return Change{}, err
		}
		if has {
			c.Packages = append(c.Packages, dir)
		} else {
			c.Deleted = append(c.Deleted, dir)
		}
	}
	return c, nil
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

// packageDirs maps paths, slash-separated and relative to the repository top
// level, to the sorted, de-duplicated directories they are in, relative to
// the module at prefix ("." when the module is the repository root). Only
// files whose names end in ".go" count, test files included. Files outside
// the module or under a testdata directory are dropped. It does not touch
// the filesystem.
func packageDirs(paths []string, prefix string) []string {
	prefix = path.Clean(prefix)
	seen := make(map[string]bool, len(paths))
	dirs := make([]string, 0, len(paths))
	for _, p := range paths {
		if !strings.HasSuffix(p, ".go") {
			continue
		}
		p = path.Clean(p)
		if prefix != "." {
			rest, ok := strings.CutPrefix(p, prefix+"/")
			if !ok {
				continue
			}
			p = rest
		}
		dir := path.Dir(p)
		if seen[dir] || slices.Contains(strings.Split(dir, "/"), "testdata") {
			continue
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	slices.Sort(dirs)
	return dirs
}

// inNestedModule reports whether the module-relative, slash-separated dir
// lies in a module nested below root: whether dir or one of its ancestors
// below root has its own go.mod in the working tree.
func inNestedModule(root, dir string) bool {
	for d := dir; d != "."; d = path.Dir(d) {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(d), "go.mod"))
		if err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// hasGoFiles reports whether dir directly contains a regular file whose name
// ends in ".go". A directory that no longer exists has none.
func hasGoFiles(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return false, nil
		}
		return false, fmt.Errorf("reading %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".go") {
			return true, nil
		}
	}
	return false, nil
}
