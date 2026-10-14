package diff

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/rfizzle/astimate/internal/metrics"
)

// RenameSource is a baseline package that files of a head package were
// renamed from.
type RenameSource struct {
	// Dir is the baseline package's module-relative directory.
	Dir string
	// Files is how many of the head package's non-test source files were
	// renamed from it.
	Files int
}

// Renames maps the module-relative directory of each head package that
// non-test source files were renamed into to the packages they were
// renamed from, most files first, then by directory.
type Renames map[string][]RenameSource

// FindRenames returns the renames git detects (git diff -M -C, a copy from
// a file the diff also modified counting as a rename) between the
// commit mergeBase and head in the module at root: for each package, the
// other packages its non-test source files were renamed from, with sc
// saying which files are source, which are tests and which package each
// belongs to. Only a file that makes its package a package
// (metrics.PackageSource) and is not a test counts, on both sides. A
// rename from or into another module, outside root or in a module nested
// below it, is not followed. Against the working tree a moved file git does
// not track yet is not a rename; against the index (head.Staged) only
// staged renames are.
func FindRenames(ctx context.Context, root, mergeBase string, sc metrics.SourceClassifier, head Head) (Renames, error) {
	prefix, err := modulePrefix(ctx, root, mergeBase)
	if err != nil {
		return nil, err
	}
	out, err := nameStatus(ctx, root, mergeBase, head, true)
	if err != nil {
		return nil, err
	}
	pairs, err := parseRenames(out)
	if err != nil {
		return nil, fmt.Errorf("listing renames since %s: %w", mergeBase, err)
	}
	if head.Tree != "" {
		root = head.Tree
	}
	counts := make(map[string]map[string]int)
	nested := make(map[string]bool)
	for _, p := range pairs {
		from, to, ok := renamedPackages(p, prefix, sc)
		if !ok {
			continue
		}
		in, err := inNestedModule(root, to, sc, nested)
		if err != nil {
			return nil, err
		}
		if in {
			continue
		}
		if counts[to] == nil {
			counts[to] = make(map[string]int)
		}
		counts[to][from]++
	}
	renames := make(Renames, len(counts))
	for to, froms := range counts {
		renames[to] = rankSources(froms)
	}
	return renames, nil
}

// rankSources returns the sources counted in files, most files first, then
// by directory.
func rankSources(files map[string]int) []RenameSource {
	srcs := make([]RenameSource, 0, len(files))
	for _, dir := range slices.Sorted(maps.Keys(files)) {
		srcs = append(srcs, RenameSource{Dir: dir, Files: files[dir]})
	}
	// Stable, so equal counts keep the directory order.
	slices.SortStableFunc(srcs, func(a, b RenameSource) int { return b.Files - a.Files })
	return srcs
}

// parseRenames returns the renames and copies in the output of git diff
// --name-status -z -M -C, each as its old and new path; other entries are
// skipped. An entry is a status and one path, or two for a rename (R) or
// copy (C). A copy counts as a rename: git lists one only from a file the
// same diff modified, which is how a file reads when part of it was moved
// out to a new file and the rest kept its path.
func parseRenames(out string) ([][2]string, error) {
	fields := splitNUL(out)
	var pairs [][2]string
	for i := 0; i < len(fields); {
		status := fields[i]
		n := 1
		if status[0] == 'R' || status[0] == 'C' {
			n = 2
		}
		if i+n >= len(fields) {
			return nil, fmt.Errorf("parsing git diff --name-status: truncated entry %q", status)
		}
		if n == 2 {
			pairs = append(pairs, [2]string{fields[i+1], fields[i+2]})
		}
		i += n + 1
	}
	return pairs, nil
}

// renamedPackages returns the module-relative packages a renamed file moved
// from and to, its old and new path being slash paths relative to the
// repository top level and the module lying at prefix, and false unless
// both paths are in the module, in different packages, and non-test files
// that make their package a package.
func renamedPackages(p [2]string, prefix string, sc metrics.SourceClassifier) (from, to string, ok bool) {
	pkg := func(file string) (string, bool) {
		rel, in := moduleRelative(file, prefix)
		if !in {
			return "", false
		}
		sf := sc.ClassifyFile(rel)
		return sf.Package, sf.Kind == metrics.PackageSource && !sf.Test
	}
	from, okFrom := pkg(p[0])
	to, okTo := pkg(p[1])
	return from, to, okFrom && okTo && from != to
}
