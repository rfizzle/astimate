// Package typescript implements the metrics extractor for TypeScript
// modules, parsing with tree-sitter through the pure-Go runtime
// github.com/odvcencio/gotreesitter and its bundled TypeScript and TSX
// grammars, so builds need no cgo.
//
// Definitions (SPEC.md section 13):
//
//   - A module is a directory holding a package.json. Nested directories
//     with their own package.json are modules of their own and are skipped.
//   - A package is a directory of the module, identified by its slash path
//     relative to the module root ("." for the root), holding at least one
//     .ts, .tsx, .mts or .cts file that is neither a declaration file
//     (.d.ts, .d.mts, .d.cts) nor a test file. node_modules, dist, build
//     and dot-prefixed directories are skipped.
//   - A test file is named *.test.* or *.spec.* with one of those four
//     extensions, or lies under a __tests__ directory, whose files belong
//     to the package containing that directory.
//   - An internal import is a relative specifier, one matching a
//     compilerOptions.paths alias of the root tsconfig.json with its
//     extends chain applied, or a bare specifier under its baseUrl, that
//     resolves as tsc does to a TypeScript file (or a directory's
//     package.json entry or index file, or under resolveJsonModule the
//     .json file it names, or under allowJs or checkJs, when no TypeScript
//     file resolves, a JavaScript file) in another package of the module.
//     JavaScript and JSON files add nothing to source metrics.
//     node_modules is not modelled, so a bare name under baseUrl that
//     resolves only to a local JavaScript file is internal even where tsc
//     would pick an installed typed package of that name.
//     Any other bare specifier, including one under baseUrl or matching an
//     alias that resolves to no such file, is external, counted by npm
//     package name, unless it names a Node built-in, which is counted as
//     stdlib.
//   - An exported function or public method whose doc comment holds the
//     line comment //astimate:untested is left out of untested_exports,
//     as in Go. The doc comment of any overload signature of the function
//     or method counts as its own.
//
// An Extractor parses each module root once, walking every file's syntax
// tree a single time to gather the facts all metrics need, and keeps them
// in an unexported module value shared by Packages and every Extract call
// for that root; the trees and file contents are not kept. Extract
// aggregates one package's facts into RawMetrics. The same walk
// fingerprints each function's body, so Functions can list a package's
// functions for the changed-function rule without another parse. The
// counting rules are written out in testdata/ts/fixture/golden/COUNTING.md.
//
// The work is split across three internal packages this one composes:
// internal/resolve finds the module's files and resolves import specifiers
// as tsc does (package.json entries, the tsconfig.json extends chain,
// paths, baseUrl, allowJs, resolveJsonModule); internal/inspect reads and
// parses each file once and records its declarations; internal/walk, which
// inspect drives in the same pass, emits the duplication tokens and scores
// and fingerprints each function. This package groups the facts into
// packages, links the import graph, runs the duplicate finder over
// internal/lang/duptok and assembles RawMetrics.
package typescript
