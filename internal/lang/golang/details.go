package golang

import (
	"context"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/rfizzle/astimate/internal/metrics"
)

// Details returns the names behind untested_exports and the duplicate block
// locations of the package with import path pkg, from the details its most
// recent Extract on mod recorded. When nothing is recorded it runs Extract
// first, so it fails exactly when Extract would.
func (e *Extractor) Details(ctx context.Context, mod *metrics.ModuleContext, pkg string) (metrics.Details, error) {
	l, err := e.cached(ctx, mod)
	if err != nil {
		return metrics.Details{}, err
	}
	d, ok := l.detailsOf(pkg)
	if !ok {
		if _, err := e.Extract(ctx, mod, pkg); err != nil {
			return metrics.Details{}, err
		}
		d, _ = l.detailsOf(pkg)
	}
	dir := ""
	if p, ok := l.pkgs[pkg]; ok {
		dir = p.Dir
	}
	locs := make([]string, 0, len(d.dupLocations))
	for _, loc := range d.dupLocations {
		locs = append(locs, relLocation(dir, loc))
	}
	return metrics.Details{
		UntestedExports:  slices.Clone(d.untestedNames),
		UntestedExcluded: slices.Clone(d.untestedExcluded),
		DupLocations:     locs,
	}, nil
}

// relLocation renders loc as "file:start-end" with file relative to dir, in
// slash form. It falls back to the base name when dir is empty or the file
// cannot be related to it.
func relLocation(dir string, loc dupLocation) string {
	file := filepath.Base(loc.file)
	if dir != "" {
		if rel, err := filepath.Rel(dir, loc.file); err == nil {
			file = rel
		}
	}
	return filepath.ToSlash(file) + ":" + strconv.Itoa(loc.startLine) + "-" + strconv.Itoa(loc.endLine)
}
