package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/engine/internal/coverage"
	"github.com/rfizzle/astimate/internal/engine/internal/judge"
	"github.com/rfizzle/astimate/internal/metrics"
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

// DefaultCoverageTimeout bounds one coverage run when CoverageOptions sets
// no timeout.
const DefaultCoverageTimeout = coverage.DefaultTimeout

// CoverageOptions opt an operation into measuring coverage_pct, which runs
// the packages' tests (SPEC.md 6). The zero value measures nothing.
type CoverageOptions = coverage.Options

// AssessOptions shape Assess's report.
type AssessOptions struct {
	// Coverage opts into measuring coverage_pct.
	Coverage CoverageOptions
}

// RankOptions shape Rank's rows.
type RankOptions struct {
	// Sort is the sort key, one of report.SortKeys.
	Sort string
	// Top keeps only the first Top rows; 0 or less keeps every row.
	Top int
	// Coverage opts into measuring coverage_pct for every ranked package.
	Coverage CoverageOptions
}

// measureCoverage measures the coverage of ms, keyed by package
// identifier, as opts asks (coverage.Measure).
func measureCoverage(ctx context.Context, t *Target, opts CoverageOptions, ms map[string]*metrics.RawMetrics, order []string) {
	coverage.Measure(ctx, coverage.Run{Ext: t.Ext, Mod: t.Mod, Options: opts, Logger: t.logger()}, ms, order)
}

// langConfig returns the configuration t's language is evaluated with. A
// target has one extractor, so each operation resolves it once per run.
func (t *Target) langConfig() config.Effective {
	return t.Cfg.ForLanguage(t.Ext.Language())
}

// Assess extracts the metrics of t's package, measures its coverage when
// opts.Coverage asks (measureCoverage), and builds its report with the
// configuration of its language (config.Config.ForLanguage).
func Assess(ctx context.Context, t *Target, opts AssessOptions) (*report.Report, error) {
	m, err := t.Ext.Extract(ctx, t.Mod, t.ImportPath)
	if err != nil {
		return nil, err
	}
	measureCoverage(ctx, t, opts.Coverage, map[string]*metrics.RawMetrics{t.ImportPath: &m}, []string{t.ImportPath})
	det, names, err := judge.Details(ctx, t.Ext, t.Mod, t.ImportPath)
	if err != nil {
		return nil, err
	}
	eff := t.langConfig()
	r := report.Build(&report.Input{
		Language:        t.Ext.Language(),
		PackagePath:     t.Dir,
		ModulePath:      t.Mod.ModulePath,
		Metrics:         m,
		Names:           names,
		Params:          eff.Rebuild,
		ConfigVersion:   eff.Version,
		AstimateVersion: t.Version,
	})
	r.Details = t.details(&det)
	return &r, nil
}

// details returns the report details block of d, with cross-package
// occurrences named by their module-relative package path.
func (t *Target) details(d *metrics.Details) *report.Details {
	return report.NewDetails(d, func(pkg string) string { return judge.Rel(t.Mod.ModulePath, pkg) })
}

// Rank lists the packages of t's module with one Packages call, extracts
// and estimates each, then sorts and truncates the rows. It returns an
// error when listing fails or opts.Sort is not a sort key (wrapping
// report.ErrUnknownSortKey). A package that fails to extract is logged,
// left out of the rows and returned as a *PackageError in failed. With
// opts.Coverage every extracted package's coverage is measured in one run
// (measureCoverage) before the rows are estimated.
func Rank(ctx context.Context, t *Target, opts RankOptions) (rows []report.Row, failed []error, err error) {
	pkgs, err := t.Ext.Packages(t.Mod.Root)
	if err != nil {
		return nil, nil, err
	}
	logger := t.logger()
	params := t.langConfig().Rebuild
	extracted := make(map[string]*metrics.RawMetrics, len(pkgs))
	order := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		m, err := t.Ext.Extract(ctx, t.Mod, pkg)
		if err != nil {
			path := judge.Rel(t.Mod.ModulePath, pkg)
			logger.Error("extracting package failed", "path", path, "err", err)
			failed = append(failed, &PackageError{Path: path, Err: err})
			continue
		}
		extracted[pkg] = &m
		order = append(order, pkg)
	}
	measureCoverage(ctx, t, opts.Coverage, extracted, order)
	rows = make([]report.Row, 0, len(order))
	for _, pkg := range order {
		rows = append(rows, report.NewRow(judge.Rel(t.Mod.ModulePath, pkg), extracted[pkg], params))
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
// HEAD's commit, or no ref outside git or before the first commit, and t's
// tokenizer, so a check with another tokenizer can warn that token counts
// are not comparable. When t's extractor implements metrics.ModuleMetrics
// the file also holds the module-level row under metrics.ModuleRowID, and
// when it implements metrics.FunctionLister each package's functions, so a
// check against the file can compute changed_func_cognitive_max, and when
// it implements metrics.Detailer each package's duplicate blocks and
// untested exports, so the check can name what the change added first. It
// returns the path written and the number of packages, not counting that
// row.
func WriteBaseline(ctx context.Context, t *Target, out string) (path string, n int, err error) {
	pkgs, err := baseline.Collect(ctx, t.Ext, t.Mod)
	if err != nil {
		return "", 0, err
	}
	funcs, err := baseline.CollectFunctions(ctx, t.Ext, t.Mod, pkgs)
	var named map[string]baseline.Named
	if err == nil {
		named, err = baseline.CollectNamed(ctx, t.Ext, t.Mod, pkgs)
	}
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
	err = baseline.WriteContents(path, baseline.Contents{
		Ref:        ref,
		ModulePath: t.Mod.ModulePath,
		Tokenizer:  t.tokenizer(),
		Packages:   pkgs,
		Functions:  funcs,
		Named:      named,
	})
	if err != nil {
		return "", 0, err
	}
	n = len(pkgs)
	if _, ok := pkgs[metrics.ModuleRowID]; ok {
		n--
	}
	return path, n, nil
}
