package metrics

// SourceClassifier is implemented by an Extractor that can say which
// changed files move which packages' metrics, for changed-package
// detection (SPEC.md 8.4). Callers type-assert an Extractor to
// SourceClassifier; without it they cannot tell a changed package from an
// unchanged one and check every package.
type SourceClassifier interface {
	// ClassifyFile says how a change to the file at rel, a slash path
	// relative to the module root, can move the module's metrics. It looks
	// at the path only, never at the filesystem.
	ClassifyFile(rel string) SourceFile
	// IsModuleMarker reports whether a file with the base name name makes
	// its directory the root of a separate module, whose files a change
	// to this module never counts.
	IsModuleMarker(name string) bool
}

// SourceKind is what a change to a file can move.
type SourceKind uint8

const (
	// NotSource is a file whose change moves no package's metrics.
	NotSource SourceKind = iota
	// PackageSource is a file that makes its directory a package, such as
	// any Go file or a TypeScript source file that is neither a test nor a
	// declaration file. A change selects the package; when the directory
	// has no such file left, the package was deleted.
	PackageSource
	// MemberSource is a file that belongs to a package without making it
	// one, such as a TypeScript test or declaration file. A change selects
	// the package while it exists and is otherwise ignored.
	MemberSource
	// ModuleSource is a file whose change can move any package's metrics,
	// such as configuration that import resolution reads. A change selects
	// every package.
	ModuleSource
)

// SourceFile is how a change to one file can move a module's metrics.
type SourceFile struct {
	// Kind is what the change can move.
	Kind SourceKind
	// Package is the module-relative, slash-separated directory of the
	// package the file belongs to ("." for the module root) when Kind is
	// PackageSource or MemberSource, and empty otherwise.
	Package string
	// Contract reports that a change to the file can move the metrics of
	// the packages importing Package, not only Package's own, such as a
	// TypeScript declaration file, which can turn an import of it from
	// external into internal. A change then also selects Package's
	// importers in the head tree (ImporterLister). Only meaningful when
	// Kind is PackageSource or MemberSource.
	Contract bool
}
