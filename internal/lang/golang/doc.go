// Package golang implements the metrics extractor for Go modules using
// go/packages, go/types and go/ast.
//
// An Extractor loads each module root once, with syntax and full type
// information for the module and its dependencies, and keeps the result in
// an unexported loaded value shared by Packages and every Extract call for
// that root. Extract also stores the value in ModuleContext.Cache so later
// calls on the same context reuse it directly.
//
// Each metric is computed by an unexported function of the shape
//
//	func <name>(l *loaded, p *packages.Package) T
//
// where p is the non-test package being measured and l gives access to the
// module-wide data: the file set, the module path, every module package by
// import path, the internal and external test packages of p, and module-wide
// indexes such as the reverse import graph. A module-wide index is built once
// per load and stored in its slot on loaded, never recomputed per package.
// Extract calls the metric functions and assembles RawMetrics from their
// results.
package golang
