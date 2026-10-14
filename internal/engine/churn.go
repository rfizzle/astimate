package engine

import (
	"context"
	"errors"

	"github.com/rfizzle/astimate/internal/churn"
	"github.com/rfizzle/astimate/internal/engine/internal/judge"
	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/metrics"
)

// MeasureChurn reads the churn of t's module once, over the window of t's
// configuration (config.Churn), and returns a function that sets
// churn_commits and churn_authors on m, the metrics of the package with
// identifier pkg (SPEC.md 6.6). The history is that of the repository
// holding t's module root, at its HEAD. With the window off, outside a git
// repository or before its first commit, or for an extractor that cannot
// classify files (metrics.SourceClassifier), the function leaves m alone,
// so both metrics stay null; any other git failure does the same after one
// warning log.
func MeasureChurn(ctx context.Context, t *Target) func(pkg string, m *metrics.RawMetrics) {
	none := func(string, *metrics.RawMetrics) {}
	sc, ok := t.Ext.(metrics.SourceClassifier)
	if t.Cfg == nil || t.Cfg.Churn.Commits <= 0 || !ok {
		return none
	}
	opts := churn.Options{Window: t.Cfg.Churn.Commits, Classifier: sc}
	if t.Ext.Language() == languageGo {
		opts.Generated = golang.IsGeneratedFile
	}
	ix, err := churn.Measure(ctx, t.Mod.Root, opts)
	switch {
	case errors.Is(err, churn.ErrUnavailable):
		t.logger().Debug("churn not measured", "err", err)
		return none
	case err != nil:
		t.logger().Warn("churn not measured", "err", err)
		return none
	}
	return func(pkg string, m *metrics.RawMetrics) {
		c := ix.Of(judge.Rel(t.Mod.ModulePath, pkg))
		m.ChurnCommits, m.ChurnAuthors = &c.Commits, &c.Authors
	}
}

// fillChurn sets the churn of every package in ms, keyed by package
// identifier (MeasureChurn), reading the history once.
func fillChurn(ctx context.Context, t *Target, ms map[string]*metrics.RawMetrics) {
	if len(ms) == 0 {
		return
	}
	fill := MeasureChurn(ctx, t)
	for pkg, m := range ms {
		fill(pkg, m)
	}
}
