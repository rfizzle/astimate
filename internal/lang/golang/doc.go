// Package golang implements the metrics extractor for Go modules using
// go/packages, go/types and go/ast.
//
// An Extractor loads each module root once (internal/load), with syntax and
// full type information for the module packages, and keeps the result in an
// unexported loaded value shared by Packages and every Extract call for that
// root. Extract also stores the value in ModuleContext.Cache so later calls
// on the same context reuse it directly.
//
// The metrics are computed by functions in packages under internal, each of
// the shape
//
//	func <Name>(m *load.Module, p *packages.Package) T
//
// where p is the non-test package being measured and m gives access to the
// module-wide data: the file set, the module path, every module package by
// import path, and the internal and external test packages of p.
// internal/inspect measures size, exported symbols, globals, complexity with
// each function's fingerprint, the opacity flags and tokens;
// internal/imports the fan-out and fan-in; internal/tests the test
// metrics and untested_exports; internal/dup duplication; internal/cover
// coverage. A module-wide index, the reverse import graph (imports.Graph)
// or the cross-package duplication pass (dup.Memo), is built once per load
// and kept in its slot on loaded, never recomputed per package. Extract
// calls the metric functions and assembles RawMetrics from their results.
package golang
