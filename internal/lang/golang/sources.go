package golang

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
)

// ClassifyFile implements metrics.SourceClassifier. Every file whose name
// ends in ".go", test files included, makes its directory a package,
// except under a testdata directory. Nothing else, go.mod included, moves
// a package's metrics. No file is marked Contract: a Go import path names
// its package whatever files it holds, so an importer's internal_imports
// moves only with the importer's own import declarations, whose change
// selects it (SPEC.md 8.4).
func (e *Extractor) ClassifyFile(rel string) metrics.SourceFile {
	if !strings.HasSuffix(rel, ".go") {
		return metrics.SourceFile{}
	}
	dir := path.Dir(path.Clean(rel))
	if slices.Contains(strings.Split(dir, "/"), "testdata") {
		return metrics.SourceFile{}
	}
	return metrics.SourceFile{Kind: metrics.PackageSource, Package: dir}
}

// IsModuleMarker implements metrics.SourceClassifier: a go.mod file starts
// a nested module.
func (e *Extractor) IsModuleMarker(name string) bool {
	return name == "go.mod"
}

// Importers implements metrics.ImporterLister: the sorted import paths of
// the module packages whose non-test files import the package with import
// path pkg, the edges fan_in counts, from the reverse import graph built
// once per load. An import path Packages does not list yields an error
// wrapping metrics.ErrUnknownPackage.
func (e *Extractor) Importers(ctx context.Context, mod *metrics.ModuleContext, pkg string) ([]string, error) {
	l, err := e.cached(ctx, mod)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("importers of %s: %w", pkg, err)
	}
	if _, ok := l.pkgs[pkg]; !ok {
		return nil, fmt.Errorf("importers of %s: %w", pkg, metrics.ErrUnknownPackage)
	}
	buildReverse(l)
	return slices.Clone(l.reverse[pkg]), nil
}
