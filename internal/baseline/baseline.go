package baseline

import (
	"context"
	"fmt"

	"github.com/rfizzle/astimate/internal/metrics"
)

// Baseline serves the metrics a change is compared against. Packages are
// matched by import path, so a renamed or moved package has no baseline and
// is treated as new.
type Baseline interface {
	// Metrics returns the baseline metrics of the package with import path
	// pkg, and false when the baseline has no such package.
	Metrics(pkg string) (metrics.RawMetrics, bool)
	// Ref names where the baseline came from: the merge-base commit for a git
	// baseline, or the ref recorded in a baseline file.
	Ref() string
	// Tokenizer names the method that counted the baseline's tokens_est:
	// the extractor's tokenizer for a baseline extracted from a commit, or
	// the tokenizer recorded in a baseline file, "est" when it records none.
	Tokenizer() string
	// Functions returns the functions of the package with import path pkg
	// as the baseline recorded them, for the function-level diff behind
	// changed_func_cognitive_max, and false when it recorded none for pkg:
	// the package is not in the baseline, the extractor does not implement
	// metrics.FunctionLister, or the baseline file predates function
	// records. A file baseline's functions carry no File or Line.
	Functions(pkg string) ([]metrics.FunctionInfo, bool)
}

// snapshot is the map-backed Baseline shared by the git and file sources.
type snapshot struct {
	ref       string
	tokenizer string
	pkgs      map[string]metrics.RawMetrics
	// funcs holds each package's functions; nil when none were recorded.
	funcs map[string][]metrics.FunctionInfo
}

// Metrics implements Baseline.
func (s *snapshot) Metrics(pkg string) (metrics.RawMetrics, bool) {
	m, ok := s.pkgs[pkg]
	return m, ok
}

// Functions implements Baseline.
func (s *snapshot) Functions(pkg string) ([]metrics.FunctionInfo, bool) {
	fns, ok := s.funcs[pkg]
	return fns, ok
}

// Ref implements Baseline.
func (s *snapshot) Ref() string { return s.ref }

// Tokenizer implements Baseline.
func (s *snapshot) Tokenizer() string { return s.tokenizer }

// Collect extracts every package ext lists under mod.Root and returns the
// metrics keyed by package identifier (the import path for Go). When ext
// implements metrics.ModuleMetrics it also adds the module-level row under
// metrics.ModuleRowID, so a check can compare module-wide metrics such as
// dup_blocks_cross_pkg against it. It stops at the first extraction error
// or when ctx is done.
func Collect(ctx context.Context, ext metrics.Extractor, mod *metrics.ModuleContext) (map[string]metrics.RawMetrics, error) {
	names, err := ext.Packages(mod.Root)
	if err != nil {
		return nil, fmt.Errorf("listing packages in %s: %w", mod.Root, err)
	}
	pkgs := make(map[string]metrics.RawMetrics, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("extracting %s: %w", name, err)
		}
		m, err := ext.Extract(ctx, mod, name)
		if err != nil {
			return nil, fmt.Errorf("extracting %s: %w", name, err)
		}
		pkgs[name] = m
	}
	if mm, ok := ext.(metrics.ModuleMetrics); ok {
		m, err := mm.ModuleRow(ctx, mod)
		if err != nil {
			return nil, fmt.Errorf("extracting %s row: %w", metrics.ModuleRowID, err)
		}
		pkgs[metrics.ModuleRowID] = m
	}
	return pkgs, nil
}

// CollectFunctions lists, with ext's metrics.FunctionLister, the functions
// of every package in pkgs except the module row, keyed like pkgs. Call it
// after Collect on the same mod, so ext answers from the extraction it just
// did. It returns nil, and no error, when ext is not a FunctionLister, and
// stops at the first listing error or when ctx is done.
func CollectFunctions(ctx context.Context, ext metrics.Extractor, mod *metrics.ModuleContext,
	pkgs map[string]metrics.RawMetrics,
) (map[string][]metrics.FunctionInfo, error) {
	fl, ok := ext.(metrics.FunctionLister)
	if !ok {
		return nil, nil
	}
	funcs := make(map[string][]metrics.FunctionInfo, len(pkgs))
	for name := range pkgs {
		if name == metrics.ModuleRowID {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("listing functions of %s: %w", name, err)
		}
		fns, err := fl.Functions(ctx, mod, name)
		if err != nil {
			return nil, fmt.Errorf("listing functions of %s: %w", name, err)
		}
		if fns == nil {
			fns = []metrics.FunctionInfo{}
		}
		funcs[name] = fns
	}
	return funcs, nil
}
