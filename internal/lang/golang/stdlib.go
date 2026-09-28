package golang

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"github.com/rfizzle/astimate/internal/metrics"
)

// ExtractStdlib computes the v0 metrics of the standard-library package at
// importPath, with its test files, under the extractor options opts. It is
// intended for the acceptance invariants (SPEC.md 7.5), which measure one
// standard-library package that belongs to no module a caller could load
// with Extract. The package is loaded on its own, so fan_in and
// fan_in_tests are 0, since no other standard-library package is in the
// load; ExtractStdlibAll measures those. Like ExtractStdlibAll it parses
// the source files of a package whose syntax is not its source, so a cgo
// package such as net, and unsafe, which go/packages gives no syntax, are
// measured from their source files. It returns an error when the load
// fails, which is how a toolchain without usable GOROOT sources shows, and
// one wrapping metrics.ErrUnknownPackage when the load yields no package at
// importPath.
func ExtractStdlib(ctx context.Context, importPath string, opts ...Option) (metrics.RawMetrics, error) {
	e := New(opts...)
	counter, err := e.counter()
	if err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", importPath, err)
	}
	m, err := load.StdPackage(ctx, e.load, importPath)
	if err != nil {
		return metrics.RawMetrics{}, err
	}
	p, ok := m.Pkgs[importPath]
	if !ok {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", importPath, metrics.ErrUnknownPackage)
	}
	return assemble(ctx, &loaded{Module: m}, p, assembleOptions{counter: counter, dup: e.dup})
}

// ExtractStdlibAll computes the v0 metrics of every package of the standard
// library (the go list pattern "std", vendored packages included), with
// its test files, under the extractor options opts. It loads the whole
// library in one go/packages call and shares that load, and so one reverse
// import graph, across every package, which makes fan_in and fan_in_tests
// count the standard-library packages importing each one. Every
// standard-library import is internal to that load, so internal_imports
// counts them and stdlib_imports is 0. Both metrics count the same import
// edges, keyed by package path, so their sums over the result are equal. It
// is intended for calibration, which pools standard-library rows with
// module rows.
//
// It returns the metrics by import path and, separately, the packages that
// failed with their errors: a package that fails to load or to extract is
// left out of the metrics. The error is non-nil only when opts are invalid
// or the library could not be loaded at all, which is how a toolchain
// without usable GOROOT sources shows.
func ExtractStdlibAll(ctx context.Context, opts ...Option) (map[string]metrics.RawMetrics, map[string]error, error) {
	got, failed, _, err := extractStdlibAll(ctx, opts...)
	return got, failed, err
}

// ExtractStdlibAllFunctions is ExtractStdlibAll that also returns, by
// import path, the functions of every measured package the way
// Extractor.Functions lists them (metrics.FunctionLister), with File
// relative to the package directory. It is intended for calibration, which
// pools the per-function cognitive complexity behind
// changed_func_cognitive_max; the metrics and failures are exactly
// ExtractStdlibAll's.
func ExtractStdlibAllFunctions(ctx context.Context, opts ...Option,
) (map[string]metrics.RawMetrics, map[string][]metrics.FunctionInfo, map[string]error, error) {
	got, failed, l, err := extractStdlibAll(ctx, opts...)
	if err != nil {
		return nil, nil, nil, err
	}
	fns := make(map[string][]metrics.FunctionInfo, len(got))
	for path := range got {
		// assemble recorded the details of every package it measured.
		d, _ := l.detailsOf(path)
		fns[path] = functionInfos(l.Fset, l.Pkgs[path].Dir, d.functions)
	}
	return got, fns, failed, nil
}

// extractStdlibAll does the work of ExtractStdlibAll and also returns the
// load, whose recorded details hold each measured package's functions.
func extractStdlibAll(ctx context.Context, opts ...Option) (map[string]metrics.RawMetrics, map[string]error, *loaded, error) {
	e := New(opts...)
	counter, err := e.counter()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("extracting std: %w", err)
	}
	m, failed, err := load.Stdlib(ctx, e.load)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(m.Paths) == 0 {
		return nil, nil, nil, fmt.Errorf("loading std: %w", ErrNoPackages)
	}
	l := &loaded{Module: m}

	results := make([]metrics.RawMetrics, len(l.Paths))
	errs := make([]error, len(l.Paths))
	next := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(l.Paths)) {
		wg.Go(func() {
			for i := range next {
				results[i], errs[i] = assemble(ctx, l, l.Pkgs[l.Paths[i]],
					assembleOptions{counter: counter, dup: e.dup})
			}
		})
	}
	for i := range l.Paths {
		next <- i
	}
	close(next)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, fmt.Errorf("extracting std: %w", err)
	}

	got := make(map[string]metrics.RawMetrics, len(l.Paths))
	for i, path := range l.Paths {
		if errs[i] != nil {
			failed[path] = errs[i]
			continue
		}
		got[path] = results[i]
	}
	return got, failed, l, nil
}
