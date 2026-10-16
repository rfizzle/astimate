package metrics

import (
	"slices"
	"strings"
)

// Skipper is an optional interface an Extractor implements when Packages
// can leave out a package that fails to load instead of failing the whole
// module. Callers type-assert an Extractor to it; without it, every
// package Packages lists is every package there is.
type Skipper interface {
	// Skipped returns the packages under root that Packages leaves out
	// because they fail to load, sorted by identifier, from the same load
	// Packages reads. It fails when Packages would.
	Skipped(root string) ([]SkippedPackage, error)
}

// SkippedPackage is a package an extractor left out of Packages because it
// fails to load, such as a Go package importing a module that is not
// available. Its neighbours are measured as if it had loaded. Package is
// the identifier Packages would list it under, Reason the kind of failure,
// such as "missing module", and Error the package's first load error.
type SkippedPackage struct {
	Package, Reason, Error string
}

// Excluder is an optional interface an Extractor implements when Packages
// can leave out a directory of source files without any load error: a Go
// directory whose files build constraints exclude entirely, which go list
// drops from "./..." without a word. Callers type-assert an Extractor to
// it, as they do to Skipper; the two lists never share a package.
type Excluder interface {
	// Excluded returns the packages under root that Packages leaves out
	// because none of their files is built, sorted by identifier, each with
	// its Reason and no Error, from the same load Packages reads. It fails
	// when Packages would.
	Excluded(root string) ([]SkippedPackage, error)
}

// NotAnalysed returns every package under root that ext's Packages leaves
// out, the ones Skipper lists (with their errors) and the ones Excluder
// lists, sorted by identifier; none when ext implements neither. It fails
// when either list does.
func NotAnalysed(ext Extractor, root string) ([]SkippedPackage, error) {
	var out []SkippedPackage
	if sk, ok := ext.(Skipper); ok {
		list, err := sk.Skipped(root)
		if err != nil {
			return nil, err
		}
		out = append(out, list...)
	}
	if ex, ok := ext.(Excluder); ok {
		list, err := ex.Excluded(root)
		if err != nil {
			return nil, err
		}
		out = append(out, list...)
	}
	slices.SortFunc(out, func(a, b SkippedPackage) int { return strings.Compare(a.Package, b.Package) })
	return out, nil
}
