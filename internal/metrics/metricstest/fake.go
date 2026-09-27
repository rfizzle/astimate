package metricstest

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"

	"github.com/rfizzle/astimate/internal/metrics"
)

// NewFake returns an in-memory metrics.Extractor for language lang that
// serves the records in pkgs. Use it wherever code needs an Extractor but not
// a real module: the estimate, the gate, the CLI and registry tests.
//
// Detect reports true only for root (compared after filepath.Clean).
// Packages returns the keys of pkgs sorted, and an error for any other root.
// Extract returns the stored record, an error for an unknown package, and
// ctx.Err() when the context is done. The map is copied, so later changes by
// the caller do not affect the fake.
func NewFake(lang, root string, pkgs map[string]metrics.RawMetrics) metrics.Extractor {
	return &fake{
		lang: lang,
		root: filepath.Clean(root),
		pkgs: maps.Clone(pkgs),
	}
}

type fake struct {
	lang string
	root string
	pkgs map[string]metrics.RawMetrics
}

func (f *fake) Language() string { return f.lang }

func (f *fake) Detect(root string) bool { return filepath.Clean(root) == f.root }

func (f *fake) Packages(root string) ([]string, error) {
	if !f.Detect(root) {
		return nil, fmt.Errorf("fake %s extractor: listing packages: %s is not the configured root %s",
			f.lang, root, f.root)
	}
	return slices.Sorted(maps.Keys(f.pkgs)), nil
}

func (f *fake) Extract(ctx context.Context, _ *metrics.ModuleContext, pkg string) (metrics.RawMetrics, error) {
	if err := ctx.Err(); err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("fake %s extractor: extracting %s: %w", f.lang, pkg, err)
	}
	m, ok := f.pkgs[pkg]
	if !ok {
		return metrics.RawMetrics{}, fmt.Errorf("fake %s extractor: unknown package %q", f.lang, pkg)
	}
	return m, nil
}
