package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
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
const DefaultCoverageTimeout = 2 * time.Minute

// CoverageOptions opt an operation into measuring coverage_pct, which runs
// the packages' tests (SPEC.md 6). The zero value measures nothing.
type CoverageOptions struct {
	// Enabled measures coverage after extraction, with one test run over
	// every package the operation reports that has test files, when the
	// target's extractor implements metrics.CoverageMeasurer.
	Enabled bool
	// Timeout bounds that run; 0 or less means DefaultCoverageTimeout.
	Timeout time.Duration
}

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

// measureCoverage sets CoveragePct in ms, the extracted metrics keyed by
// package identifier, when opts.Enabled, from one metrics.CoverageMeasurer
// run over the packages with test files, bounded by opts.Timeout. The
// others keep a null coverage_pct: the packages without test files, and
// every package when t's extractor cannot measure coverage, which is
// logged once. A package whose tests fail to build or run, or that the run
// did not reach, is logged with the reason, once per package; its other
// metrics are unaffected.
func measureCoverage(ctx context.Context, t *Target, opts CoverageOptions, ms map[string]*metrics.RawMetrics, order []string) {
	if !opts.Enabled || len(ms) == 0 {
		return
	}
	logger := t.logger()
	cm, ok := t.Ext.(metrics.CoverageMeasurer)
	if !ok {
		logger.Warn("coverage not measured", "language", t.Ext.Language(),
			"reason", "the extractor cannot run tests; coverage_pct stays null")
		return
	}
	pkgs := make([]string, 0, len(order))
	for _, pkg := range order {
		if m, ok := ms[pkg]; ok && m.TestFiles > 0 {
			pkgs = append(pkgs, pkg)
		}
	}
	if len(pkgs) == 0 {
		return
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultCoverageTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := cm.Coverage(cctx, t.Mod, pkgs)
	timedOut := errors.Is(cctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	for _, pkg := range pkgs {
		c := res[pkg]
		if err != nil {
			c = metrics.Coverage{Reason: err.Error()}
		}
		if c.Pct != nil {
			v := *c.Pct
			ms[pkg].CoveragePct = &v
			continue
		}
		if c.Reason == "" {
			continue
		}
		attrs := []any{"path", modulePathRel(t.Mod.ModulePath, pkg), "reason", c.Reason}
		if timedOut {
			attrs = append(attrs, "timeout", timeout.String())
		}
		logger.Warn("coverage not measured", attrs...)
	}
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
	det, err := packageDetails(ctx, t.Ext, t.Mod, t.ImportPath)
	if err != nil {
		return nil, err
	}
	eff := t.langConfig()
	r := report.Build(&report.Input{
		Language:        t.Ext.Language(),
		PackagePath:     t.Dir,
		ModulePath:      t.Mod.ModulePath,
		Metrics:         m,
		Names:           namesOf(&det),
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
	return report.NewDetails(d, func(pkg string) string { return modulePathRel(t.Mod.ModulePath, pkg) })
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
			path := modulePathRel(t.Mod.ModulePath, pkg)
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
		rows = append(rows, report.NewRow(modulePathRel(t.Mod.ModulePath, pkg), extracted[pkg], params))
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
// check against the file can compute changed_func_cognitive_max. It
// returns the path written and the number of packages, not counting that
// row.
func WriteBaseline(ctx context.Context, t *Target, out string) (path string, n int, err error) {
	pkgs, err := baseline.Collect(ctx, t.Ext, t.Mod)
	if err != nil {
		return "", 0, err
	}
	funcs, err := baseline.CollectFunctions(ctx, t.Ext, t.Mod, pkgs)
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
