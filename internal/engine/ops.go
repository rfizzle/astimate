package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/report"
)

// DefaultBaselinePath is where WriteBaseline writes, relative to the module
// root, when no output path is given.
const DefaultBaselinePath = baseline.DefaultPath

// PackageError is the failure of one package in a module-wide operation.
// The operation logs it, leaves the package out and goes on with the rest.
type PackageError struct {
	// Path is the package directory relative to the module root, in slash
	// form.
	Path string
	// Err is the cause.
	Err error
}

// Error returns the package path and the cause.
func (e *PackageError) Error() string {
	return e.Path + ": " + e.Err.Error()
}

// Unwrap returns the cause.
func (e *PackageError) Unwrap() error { return e.Err }

// RankOptions shape Rank's rows.
type RankOptions struct {
	// Sort is the sort key, one of report.SortKeys.
	Sort string
	// Top keeps only the first Top rows; 0 or less keeps every row.
	Top int
}

// Assess extracts the metrics of t's package and builds its report.
func Assess(ctx context.Context, t *Target) (*report.Report, error) {
	m, err := t.Ext.Extract(ctx, t.Mod, t.ImportPath)
	if err != nil {
		return nil, err
	}
	names, err := Names(ctx, t, t.ImportPath)
	if err != nil {
		return nil, err
	}
	r := report.Build(&report.Input{
		Language:        t.Ext.Language(),
		PackagePath:     t.Dir,
		ModulePath:      t.Mod.ModulePath,
		Metrics:         m,
		Names:           names,
		Params:          t.Cfg.Rebuild,
		ConfigVersion:   t.Cfg.Version,
		AstimateVersion: t.Version,
	})
	return &r, nil
}

// Rank lists the packages of t's module with one Packages call, extracts
// and estimates each, then sorts and truncates the rows. It returns an
// error when listing fails or opts.Sort is not a sort key (wrapping
// report.ErrUnknownSortKey). A package that fails to extract is logged,
// left out of the rows and returned as a *PackageError in failed.
func Rank(ctx context.Context, t *Target, opts RankOptions) (rows []report.Row, failed []error, err error) {
	pkgs, err := t.Ext.Packages(t.Mod.Root)
	if err != nil {
		return nil, nil, err
	}
	logger := t.logger()
	rows = make([]report.Row, 0, len(pkgs))
	for _, pkg := range pkgs {
		path := modulePathRel(t.Mod.ModulePath, pkg)
		m, err := t.Ext.Extract(ctx, t.Mod, pkg)
		if err != nil {
			logger.Error("extracting package failed", "path", path, "err", err)
			failed = append(failed, &PackageError{Path: path, Err: err})
			continue
		}
		rows = append(rows, report.NewRow(path, &m, t.Cfg.Rebuild))
	}
	if err := report.SortRows(rows, opts.Sort); err != nil {
		return nil, failed, err
	}
	if opts.Top > 0 && opts.Top < len(rows) {
		rows = rows[:opts.Top]
	}
	return rows, failed, nil
}

// WriteBaseline extracts every package of t's module and writes them as a
// baseline file to out, or to DefaultBaselinePath under the module root when
// out is empty, creating the file's directory if missing. The file records
// HEAD's commit, or no ref outside git or before the first commit. It
// returns the path written and the number of packages.
func WriteBaseline(ctx context.Context, t *Target, out string) (path string, n int, err error) {
	pkgs, err := baseline.Collect(ctx, t.Ext, t.Mod)
	if err != nil {
		return "", 0, err
	}
	// Outside git, or before the first commit, the file records no ref.
	ref, _ := baseline.HeadCommit(ctx, t.Mod.Root)

	path = out
	if path == "" {
		path = filepath.Join(t.Mod.Root, baseline.DefaultPath)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", 0, fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := baseline.Write(path, ref, t.Mod.ModulePath, pkgs); err != nil {
		return "", 0, err
	}
	return path, len(pkgs), nil
}
