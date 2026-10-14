// Package coverage measures coverage_pct and untested_exports_covered for
// the packages an operation extracted, in one run of the extractor's
// metrics.CoverageMeasurer, and logs each package it could not measure.
// Package engine composes it after extraction; it selects no package and
// judges no row.
package coverage

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/rfizzle/astimate/internal/engine/internal/judge"
	"github.com/rfizzle/astimate/internal/metrics"
)

// DefaultTimeout bounds one coverage run when Options sets no timeout.
const DefaultTimeout = 2 * time.Minute

// Options opt an operation into measuring coverage_pct, which runs the
// packages' tests (SPEC.md 6). The zero value measures nothing.
type Options struct {
	// Enabled measures coverage after extraction, with one test run over
	// every package the operation reports that has test files, when the
	// target's extractor implements metrics.CoverageMeasurer.
	Enabled bool
	// Timeout bounds that run; 0 or less means DefaultTimeout.
	Timeout time.Duration
}

// Run is what Measure measures with.
type Run struct {
	// Ext is the extractor, measured with when it implements
	// metrics.CoverageMeasurer, and Mod the module it runs in.
	Ext metrics.Extractor
	Mod *metrics.ModuleContext
	// Options say whether to measure and how long the run may take.
	Options
	// Logger receives one warning per package not measured.
	Logger *slog.Logger
}

// Measure sets CoveragePct and UntestedExportsCovered in ms, the
// extracted metrics keyed by package identifier, when r.Enabled, from one
// metrics.CoverageMeasurer call over the packages with test files or
// untested exports, whose tests other packages' tests may run, bounded by
// r.Timeout. The others keep both null: the packages with neither, and
// every package when r.Ext cannot measure coverage, which is logged once.
// A package whose tests fail to build or run, or that the run did not
// reach, is logged with the reason, once per package; its other metrics
// are unaffected. order is the order the packages are measured and logged
// in.
func Measure(ctx context.Context, r Run, ms map[string]*metrics.RawMetrics, order []string) {
	if !r.Enabled || len(ms) == 0 {
		return
	}
	cm, ok := r.Ext.(metrics.CoverageMeasurer)
	if !ok {
		r.Logger.Warn("coverage not measured", "language", r.Ext.Language(),
			"reason", "the extractor cannot run tests; coverage_pct stays null")
		return
	}
	pkgs := candidates(ms, order)
	if len(pkgs) == 0 {
		return
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := cm.Coverage(cctx, r.Mod, pkgs)
	timedOut := errors.Is(cctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	for _, pkg := range pkgs {
		c := res[pkg]
		if err != nil {
			c = metrics.Coverage{Reason: err.Error()}
		}
		reason := apply(ms[pkg], c)
		if reason == "" {
			continue
		}
		attrs := []any{"path", judge.Rel(r.Mod.ModulePath, pkg), "reason", reason}
		if timedOut {
			attrs = append(attrs, "timeout", timeout.String())
		}
		r.Logger.Warn("coverage not measured", attrs...)
	}
}

// candidates returns, in the order of order, the packages of ms a
// coverage run can say something about: those with test files, and those
// with untested exports, which other packages' tests may run.
func candidates(ms map[string]*metrics.RawMetrics, order []string) []string {
	pkgs := make([]string, 0, len(order))
	for _, pkg := range order {
		if m, ok := ms[pkg]; ok && (m.TestFiles > 0 || m.UntestedExports > 0) {
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs
}

// apply sets m's coverage_pct and untested_exports_covered from c,
// copying each value it has, and returns the reason coverage_pct stays
// null when c gives one; empty when it was measured or nothing failed.
func apply(m *metrics.RawMetrics, c metrics.Coverage) string {
	if c.UntestedExports != nil {
		n := *c.UntestedExports
		m.UntestedExportsCovered = &n
	}
	if c.Pct != nil {
		v := *c.Pct
		m.CoveragePct = &v
		return ""
	}
	return c.Reason
}
