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
//
// With WithDetails the fake also implements metrics.Detailer; without it, it
// does not, so callers exercise their fallback to counts alone. Likewise,
// WithModuleRow makes it implement metrics.ModuleMetrics.
func NewFake(lang, root string, pkgs map[string]metrics.RawMetrics, opts ...FakeOption) metrics.Extractor {
	f := &fake{
		lang: lang,
		root: filepath.Clean(root),
		pkgs: maps.Clone(pkgs),
	}
	for _, opt := range opts {
		opt(f)
	}
	switch {
	case f.details != nil && f.row != nil:
		return &detailModuleFake{fake: f}
	case f.details != nil:
		return &detailFake{fake: f}
	case f.row != nil:
		return &moduleFake{fake: f}
	}
	return f
}

// FakeOption configures the extractor NewFake returns.
type FakeOption func(*fake)

// WithDetails makes the fake implement metrics.Detailer, serving details[pkg]
// for each package. Details returns the zero value for a package of the fake
// missing from details, an error for an unknown package, and ctx.Err() when
// the context is done. The map is copied.
func WithDetails(details map[string]metrics.Details) FakeOption {
	return func(f *fake) {
		f.details = maps.Clone(details)
		if f.details == nil {
			f.details = map[string]metrics.Details{}
		}
	}
}

// WithModuleRow makes the fake implement metrics.ModuleMetrics, serving row
// as the module row. ModuleRow returns ctx.Err() when the context is done.
func WithModuleRow(row metrics.RawMetrics) FakeOption {
	return func(f *fake) { f.row = &row }
}

type fake struct {
	lang    string
	root    string
	pkgs    map[string]metrics.RawMetrics
	details map[string]metrics.Details
	row     *metrics.RawMetrics
}

// detailFake is a fake that also implements metrics.Detailer.
type detailFake struct {
	*fake
}

func (f *detailFake) Details(ctx context.Context, mod *metrics.ModuleContext, pkg string) (metrics.Details, error) {
	return f.detailsOf(ctx, mod, pkg)
}

// moduleFake is a fake that also implements metrics.ModuleMetrics.
type moduleFake struct {
	*fake
}

func (f *moduleFake) ModuleRow(ctx context.Context, mod *metrics.ModuleContext) (metrics.RawMetrics, error) {
	return f.moduleRow(ctx, mod)
}

// detailModuleFake is a fake that implements both metrics.Detailer and
// metrics.ModuleMetrics.
type detailModuleFake struct {
	*fake
}

func (f *detailModuleFake) Details(ctx context.Context, mod *metrics.ModuleContext, pkg string) (metrics.Details, error) {
	return f.detailsOf(ctx, mod, pkg)
}

func (f *detailModuleFake) ModuleRow(ctx context.Context, mod *metrics.ModuleContext) (metrics.RawMetrics, error) {
	return f.moduleRow(ctx, mod)
}

func (f *fake) moduleRow(ctx context.Context, _ *metrics.ModuleContext) (metrics.RawMetrics, error) {
	if err := ctx.Err(); err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("fake %s extractor: extracting %s: %w", f.lang, metrics.ModuleRowID, err)
	}
	return *f.row, nil
}

func (f *fake) detailsOf(ctx context.Context, _ *metrics.ModuleContext, pkg string) (metrics.Details, error) {
	if err := ctx.Err(); err != nil {
		return metrics.Details{}, fmt.Errorf("fake %s extractor: details of %s: %w", f.lang, pkg, err)
	}
	if _, ok := f.pkgs[pkg]; !ok {
		return metrics.Details{}, fmt.Errorf("fake %s extractor: unknown package %q", f.lang, pkg)
	}
	d := f.details[pkg]
	return metrics.Details{
		UntestedExports:  slices.Clone(d.UntestedExports),
		UntestedExcluded: slices.Clone(d.UntestedExcluded),
		DupLocations:     slices.Clone(d.DupLocations),
	}, nil
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
