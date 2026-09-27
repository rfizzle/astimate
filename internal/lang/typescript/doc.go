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
//     extends chain applied, or a bare specifier naming a path under its
//     baseUrl, that resolves to another package of the module. Any other
//     bare specifier is external, counted by npm package name, unless it
//     names a Node built-in, which is counted as stdlib.
//   - An exported function or public method whose doc comment holds the
//     line comment //astimate:untested is left out of untested_exports,
//     as in Go.
//
// An Extractor parses each module root once, walking every file's syntax
// tree a single time to gather the facts all metrics need, and keeps them
// in an unexported module value shared by Packages and every Extract call
// for that root; the trees and file contents are not kept. Extract
// aggregates one package's facts into RawMetrics. The counting rules are
// written out in testdata/ts/fixture/golden/COUNTING.md.
package typescript
