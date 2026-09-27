package golang

import (
	"path"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
)

// ClassifyFile implements metrics.SourceClassifier. Every file whose name
// ends in ".go", test files included, makes its directory a package,
// except under a testdata directory. Nothing else, go.mod included, moves
// a package's metrics.
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
