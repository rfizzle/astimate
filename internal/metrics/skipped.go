package metrics

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
