package golang

import (
	"context"
	"fmt"
	"go/token"

	"github.com/rfizzle/astimate/internal/metrics"
	"golang.org/x/tools/go/packages"
)

// stdModulePath is the module path the standard library reports.
const stdModulePath = "std"

// ExtractStdlib computes the v0 metrics of the standard-library package at
// importPath, with its test files, under the extractor options opts. It is
// intended for the acceptance invariants (SPEC.md 7.5) and for calibration,
// which measure standard-library packages that belong to no module a caller
// could load with Extract. The package is loaded on its own, so fan_in and
// fan_in_tests are 0: no other standard-library package is in the load. It
// returns an error when the load fails, which is how a toolchain without
// usable GOROOT sources shows, and one wrapping ErrUnknownPackage when the
// load yields no package at importPath.
func ExtractStdlib(ctx context.Context, importPath string, opts ...Option) (metrics.RawMetrics, error) {
	e := New(opts...)
	counter, err := e.counter()
	if err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", importPath, err)
	}
	cfg := &packages.Config{Context: ctx, Mode: loadMode, Tests: true, Fset: token.NewFileSet()}
	roots, err := e.load(cfg, importPath)
	if err != nil {
		// go/packages flattens a cancellation into its message; report the
		// context's own error so callers can match it.
		if cerr := ctx.Err(); cerr != nil {
			err = cerr
		}
		return metrics.RawMetrics{}, fmt.Errorf("loading %s: %w", importPath, err)
	}
	// index keeps the packages under its module path argument, so importPath
	// selects the package and its test variants and drops the test main.
	l, err := index(importPath, roots)
	if err != nil {
		return metrics.RawMetrics{}, err
	}
	p, ok := l.pkgs[importPath]
	if !ok {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", importPath, ErrUnknownPackage)
	}
	l.modulePath = stdModulePath
	l.fset = cfg.Fset
	return assemble(ctx, l, p, assembleOptions{counter: counter, dup: e.dup})
}
