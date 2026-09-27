package metrics

import "context"

// ImporterLister is implemented by an Extractor that can list the module
// packages importing a package, so that changed-package detection can
// select the importers of a package whose changed file can move theirs
// (SourceFile.Contract, SPEC.md 8.4). Callers type-assert an Extractor to
// ImporterLister; without it they check the changed packages alone.
type ImporterLister interface {
	// Importers returns the sorted identifiers, as Packages lists them,
	// of the module packages whose non-test files import pkg: the edges
	// fan_in counts, from the same load Extract reads. An identifier
	// Packages does not list yields an error wrapping ErrUnknownPackage.
	Importers(ctx context.Context, mod *ModuleContext, pkg string) ([]string, error)
}
