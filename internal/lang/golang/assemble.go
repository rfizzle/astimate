package golang

import (
	"context"
	"fmt"

	"github.com/rfizzle/astimate/internal/metrics"
	"golang.org/x/tools/go/packages"
)

// assembleOptions carries the extractor configuration assemble needs.
type assembleOptions struct {
	// counter counts tokens_est and tokens_est_with_tests.
	counter tokenCounter
	// dup configures duplicate detection.
	dup dupOptions
}

// details is the per-package debug record the metric functions produce
// beside the counts: the names behind untested_exports, the duplicate block
// locations, the blank and dot imports, and the token counting method.
type details struct {
	untestedNames, untestedExcluded []string
	dupLocations                    []dupLocation
	blank, dot                      []string
	tokensMethod                    string
}

// assemble computes every v0 metric of p in l and maps it into RawMetrics by
// its SPEC.md section 6 name, leaving the v1 fields nil. It records the
// debug details of p in l. size runs before duplication, which weighs lines
// by it, and ctx is checked before each of the expensive steps, duplication
// and tokens. Errors are wrapped with p's import path.
func assemble(ctx context.Context, l *loaded, p *packages.Package, opts assembleOptions) (metrics.RawMetrics, error) {
	imp := imports(l, p)
	fi := fanIn(l, p)
	gl := globals(l, p)
	cx := complexity(l, p)
	ts := testMetrics(l, p)
	un := untestedExports(l, p)
	sz, err := size(l, p)
	if err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", p.PkgPath, err)
	}
	if err := ctx.Err(); err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", p.PkgPath, err)
	}
	dup, err := duplication(l, p, sz, opts.dup)
	if err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", p.PkgPath, err)
	}
	if err := ctx.Err(); err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", p.PkgPath, err)
	}
	tok, err := tokens(l, p, opts.counter)
	if err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", p.PkgPath, err)
	}

	l.setDetails(p.PkgPath, details{
		untestedNames:    un.names,
		untestedExcluded: un.excluded,
		dupLocations:     dup.locations,
		blank:            imp.blank,
		dot:              imp.dot,
		tokensMethod:     tok.method,
	})
	return metrics.RawMetrics{
		Files:              sz.files,
		SLOC:               sz.sloc,
		LargestFileSLOC:    sz.largestFileSLOC,
		TokensEst:          tok.tokensEst,
		TokensEstWithTests: tok.tokensEstWithTests,
		InternalImports:    imp.internal,
		ExternalImports:    imp.external,
		StdlibImports:      imp.stdlib,
		FanIn:              fi.fanIn,
		FanInTests:         fi.fanInTests,
		ExportedSymbols:    sz.exportedSymbols,
		Globals:            gl.globals,
		InitFuncs:          gl.initFuncs,
		MaxNesting:         cx.maxNesting,
		CognitiveTotal:     cx.cognitiveTotal,
		CognitiveP90:       cx.cognitiveP90,
		FuncCount:          cx.funcCount,
		DupBlocks:          dup.blocks,
		DuplicationPct:     dup.pct,
		TestFiles:          ts.testFiles,
		TestFuncs:          ts.testFuncs,
		HasTests:           ts.hasTests,
		UntestedExports:    un.untested,
	}, nil
}

// setDetails records d as the most recent details of the package at
// importPath.
func (l *loaded) setDetails(importPath string, d details) {
	l.detailsMu.Lock()
	defer l.detailsMu.Unlock()
	if l.details == nil {
		l.details = make(map[string]details)
	}
	l.details[importPath] = d
}

// detailsOf returns the most recent details recorded for the package at
// importPath, and whether any were.
func (l *loaded) detailsOf(importPath string) (details, bool) {
	l.detailsMu.Lock()
	defer l.detailsMu.Unlock()
	d, ok := l.details[importPath]
	return d, ok
}
