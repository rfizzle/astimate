package golang

import (
	"context"
	"fmt"

	"github.com/rfizzle/astimate/internal/lang/duptok"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/dup"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/imports"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/inspect"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/tests"
	"github.com/rfizzle/astimate/internal/metrics"
	"golang.org/x/tools/go/packages"
)

// assembleOptions carries the extractor configuration assemble needs.
type assembleOptions struct {
	// counter counts tokens_est and tokens_est_with_tests.
	counter inspect.Counter
	// dup configures duplicate detection.
	dup dup.Options
	// files is where source bytes are read from; nil means the disk. It is
	// wrapped in a load.FileCache per call, so each file is opened at most
	// once.
	files load.FileSource
}

// results holds the per-package results of every metric function, before
// assemble maps them into RawMetrics.
type results struct {
	imp      imports.Counts
	fanIn    imports.FanIn
	globals  inspect.GlobalCounts
	cx       inspect.ComplexityCounts
	tests    tests.Counts
	untested tests.UntestedCounts
	size     inspect.SizeCounts
	dup      duptok.Result
	// crossPkg is dup_blocks_cross_pkg, nil for the standard-library loads.
	crossPkg *int
	opacity  inspect.OpacityFlags
	tokens   inspect.TokenCounts
}

// assemble computes every v0 metric of p in l and maps it into RawMetrics by
// its SPEC.md section 6 name, with the v1 fields instability, abstractness
// and main_sequence_distance derived from them (nil when undefined, else
// rounded to three decimal places), the opacity flags uses_cgo,
// uses_reflect and generated_files, tokens_est_generated, and
// dup_blocks_cross_pkg (nil for the standard-library loads, which are not
// modules); coverage_pct and changed_func_cognitive_max are left nil. It
// records the debug details of p in l. Errors are wrapped with p's import
// path.
func assemble(ctx context.Context, l *loaded, p *packages.Package, opts assembleOptions) (metrics.RawMetrics, error) {
	r, err := measure(ctx, l, p, opts)
	if err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", p.PkgPath, err)
	}

	// Martin's package metrics, from the fan-in, import and size results
	// above: Ca = fan_in and Ce = internal_imports, both module-internal
	// edges only. Reported, not gated. Each ratio is rounded to three
	// decimal places, the distance computed from the unrounded ratios, so
	// none carries float noise.
	instability := metrics.Instability(r.fanIn.FanIn, r.imp.Internal)
	abstractness := metrics.Abstractness(r.size.Exports.InterfaceTypes, r.size.Exports.Types)
	distance := metrics.RoundRatio(metrics.MainSequenceDistance(abstractness, instability))
	instability = metrics.RoundRatio(instability)
	abstractness = metrics.RoundRatio(abstractness)

	l.setDetails(p.PkgPath, details{
		untested:     r.untested,
		globals:      r.globals,
		imp:          r.imp,
		dupLocations: r.dup.Locations,
		tokensMethod: r.tokens.Method,
		functions:    r.cx.PerFunc,
		largestFile:  r.size.LargestFile,
		files:        p.GoFiles,
	})
	return metrics.RawMetrics{
		Files:              r.size.Files,
		SLOC:               r.size.SLOC,
		LargestFileSLOC:    r.size.LargestFileSLOC,
		TokensEst:          r.tokens.TokensEst,
		TokensEstWithTests: r.tokens.TokensEstWithTests,
		InternalImports:    r.imp.Internal,
		ExternalImports:    r.imp.External,
		StdlibImports:      r.imp.Stdlib,
		FanIn:              r.fanIn.FanIn,
		FanInTests:         r.fanIn.FanInTests,
		ExportedSymbols:    r.size.Exports.Symbols,
		Globals:            r.globals.Globals,
		InitFuncs:          r.globals.InitFuncs,
		MaxNesting:         r.cx.MaxNesting,
		CognitiveTotal:     r.cx.CognitiveTotal,
		CognitiveP90:       r.cx.CognitiveP90,
		FuncCount:          r.cx.FuncCount,
		DupBlocks:          r.dup.Blocks,
		DuplicationPct:     r.dup.Pct,
		TestFiles:          r.tests.TestFiles,
		TestFuncs:          r.tests.TestFuncs,
		HasTests:           r.tests.HasTests,
		UntestedExports:    r.untested.Untested,

		Instability:          instability,
		Abstractness:         abstractness,
		MainSequenceDistance: distance,
		DupBlocksCrossPkg:    r.crossPkg,
		UsesCgo:              &r.opacity.Cgo,
		UsesReflect:          &r.opacity.Reflect,
		GeneratedFiles:       &r.opacity.Generated,
		TokensEstGenerated:   &r.tokens.TokensEstGenerated,
	}, nil
}

// measure runs every metric function on p in l. Complexity runs before
// Globals, which reads the variable writes its walk records. Size runs before
// duplication, which weighs lines by it, and ctx is checked after each of
// the expensive steps, duplication and cross-package duplication, before
// the next. Size, duplication and tokens share one load.FileCache, so each
// file is opened at most once. The module-wide cross-package pass runs once
// per load (see dup.Memo); the call that runs it reads every module file
// through the same cache, so p's own files are still opened once.
func measure(ctx context.Context, l *loaded, p *packages.Package, opts assembleOptions) (r results, err error) {
	m := l.Module
	r.imp = imports.Count(m, p)
	r.fanIn = l.graph.FanIn(m, p)
	r.cx = inspect.Complexity(m, p)
	r.globals = inspect.Globals(m, p, r.cx.Written)
	r.tests = tests.Count(m, p)
	r.untested = tests.Untested(m, p)
	next := opts.files
	if next == nil {
		next = load.OSFiles{}
	}
	src := load.NewFileCache(next)
	if r.size, err = inspect.Size(m, p, src); err != nil {
		return r, err
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	if r.dup, err = dup.Package(m, p, src, r.size.SLOC, opts.dup); err != nil {
		return r, err
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	if dup.Applies(m) {
		cross, err := l.cross.Cross(m, src, opts.dup)
		if err != nil {
			return r, err
		}
		n := cross.PerPkg[p.PkgPath]
		r.crossPkg = &n
		if err := ctx.Err(); err != nil {
			return r, err
		}
	}
	r.opacity = inspect.Opacity(m, p)
	r.tokens, err = inspect.Tokens(m, p, src, opts.counter)
	return r, err
}
