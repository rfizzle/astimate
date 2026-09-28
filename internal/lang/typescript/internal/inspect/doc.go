// Package inspect is the per-file pass of the TypeScript extractor: a
// Scanner reads each file once, parses it with tree-sitter through the
// pure-Go runtime github.com/odvcencio/gotreesitter, and walks the tree a
// single time, recording the top-level declarations itself (exports,
// classes, globals, module initialization, test cases, the untested
// directive) and handing everything below to package walk, which emits the
// duplication tokens and scores and fingerprints each function. The
// result, a Facts value, keeps neither the tree nor the file's bytes.
package inspect
