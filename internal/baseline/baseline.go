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
}

// snapshot is the map-backed Baseline shared by the git and file sources.
type snapshot struct {
	ref       string
	tokenizer string
	pkgs      map[string]metrics.RawMetrics
}

// Metrics implements Baseline.
func (s *snapshot) Metrics(pkg string) (metrics.RawMetrics, bool) {
	m, ok := s.pkgs[pkg]
	return m, ok
}

// Ref implements Baseline.
func (s *snapshot) Ref() string { return s.ref }

// Tokenizer implements Baseline.
func (s *snapshot) Tokenizer() string { return s.tokenizer }

// Collect extracts every package ext lists under mod.Root and returns the
// metrics keyed by package identifier (the import path for Go). It stops at
// the first extraction error or when ctx is done.
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
	return pkgs, nil
}
