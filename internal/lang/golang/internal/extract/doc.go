// Package extract is the Go extractor's extraction proper: the module
// context a load is measured in and the assembly of one package's
// RawMetrics and debug details from the metric functions.
//
// A Module wraps one load (internal/load) with the module-wide indexes built
// from it at most once, however many packages are measured: the reverse
// import graph (imports.Graph), the test reference index (tests.Refs) and
// the cross-package duplication pass (dup.Memo). Assemble runs the metric
// functions of internal/inspect, internal/imports, internal/tests and
// internal/dup on one package, maps their results into RawMetrics with the
// derived fields and the package's role, and records the package's
// Details on the Module for the extractor's Details and Functions. The
// extractor in the parent package loads and caches each Module and calls
// Assemble; the standard-library loads use the same mapping.
package extract
