package typescript

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/exclude"
	"github.com/rfizzle/astimate/internal/lang/typescript/internal/resolve"
	"github.com/rfizzle/astimate/internal/metrics"
)

// WithExclude sets the configuration's exclude list (SPEC.md 6.5): the
// module's files, test files included, whose slash path relative to the
// module root it matches are dropped from the file walk, so they are never
// read and count in no metric, and ClassifyFile reports them as moving
// nothing. A directory all of whose non-test source files are excluded is
// no package: Packages leaves it out and Excluded lists it. Each excluded
// file is logged at debug level. Nil, the default, excludes nothing.
func WithExclude(list *exclude.List) Option {
	return func(e *Extractor) { e.exclude = list }
}

// Excluded returns the packages of the module at root all of whose
// non-test source files the exclude list matches (WithExclude), sorted by
// identifier, each with the reason exclude.Reason gives the first pattern,
// in list order, that matched one of them (metrics.Excluder). It shares
// Packages' parse and fails when Packages would.
func (e *Extractor) Excluded(root string) ([]metrics.SkippedPackage, error) {
	m, err := e.module(context.Background(), root)
	if err != nil {
		return nil, err
	}
	return slices.Clone(m.excluded), nil
}

// excludeFiles returns files without those list matches, logging each of
// them, and each package left out, to logger, when set, at debug level,
// and the packages all of whose non-test files it matched, sorted by
// identifier, each with the reason naming the first pattern, in list
// order, that matched one of them.
func excludeFiles(files []resolve.File, list *exclude.List, logger *slog.Logger) ([]resolve.File, []metrics.SkippedPackage) {
	if list == nil {
		return files, nil
	}
	kept := make([]resolve.File, 0, len(files))
	hasSource := make(map[string]bool)
	first := make(map[string]int)
	for _, f := range files {
		i, pattern, ok := list.Match(f.Rel)
		if !ok {
			kept = append(kept, f)
			hasSource[f.Pkg] = hasSource[f.Pkg] || !f.Test
			continue
		}
		if logger != nil {
			logger.Debug("excluded file", "file", f.Rel, "pattern", pattern)
		}
		if j, seen := first[f.Pkg]; !f.Test && (!seen || i < j) {
			first[f.Pkg] = i
		}
	}
	var excluded []metrics.SkippedPackage
	for pkg, i := range first {
		if !hasSource[pkg] {
			excluded = append(excluded, metrics.SkippedPackage{Package: pkg, Reason: exclude.Reason(list.Patterns()[i])})
		}
	}
	slices.SortFunc(excluded, func(a, b metrics.SkippedPackage) int { return strings.Compare(a.Package, b.Package) })
	if logger != nil {
		for _, p := range excluded {
			logger.Debug("skipped package", "package", p.Package, "reason", p.Reason)
		}
	}
	return kept, excluded
}
