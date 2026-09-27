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
	// files is where source bytes are read from; nil means the disk. It is
	// wrapped in a fileCache per call, so each file is opened at most once.
	files fileSource
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
// its SPEC.md section 6 name, with the v1 fields instability, abstractness
// and main_sequence_distance derived from them; those are nil when
// undefined, and the other v1 fields are left nil. It records the
// debug details of p in l. size runs before duplication, which weighs lines
// by it, and ctx is checked before each of the expensive steps, duplication
// and tokens. size, duplication and tokens share one fileCache, so each file
// is opened at most once. Errors are wrapped with p's import path.
func assemble(ctx context.Context, l *loaded, p *packages.Package, opts assembleOptions) (metrics.RawMetrics, error) {
	imp := imports(l, p)
	fi := fanIn(l, p)
	gl := globals(l, p)
	cx := complexity(l, p)
	ts := testMetrics(l, p)
	un := untestedExports(l, p)
	next := opts.files
	if next == nil {
		next = osFiles{}
	}
	src := newFileCache(next)
	sz, err := size(l, p, src)
	if err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", p.PkgPath, err)
	}
	if err := ctx.Err(); err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", p.PkgPath, err)
	}
	dup, err := duplication(l, p, src, sz, opts.dup)
	if err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", p.PkgPath, err)
	}
	if err := ctx.Err(); err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", p.PkgPath, err)
	}
	tok, err := tokens(l, p, src, opts.counter)
	if err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", p.PkgPath, err)
	}

	// Martin's package metrics, from the fan-in, import and size results
	// above: Ca = fan_in and Ce = internal_imports, both module-internal
	// edges only. Reported, not gated.
	instability := metrics.Instability(fi.fanIn, imp.internal)
	abstractness := metrics.Abstractness(sz.exports.interfaceTypes, sz.exports.types)

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
		ExportedSymbols:    sz.exports.symbols,
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

		Instability:          instability,
		Abstractness:         abstractness,
		MainSequenceDistance: metrics.MainSequenceDistance(abstractness, instability),
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
