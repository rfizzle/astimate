package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// IsGeneratedFile reports whether the Go file at path carries a
// generated-code header before its package clause (ast.IsGenerated), the
// rule generated_files counts by (SPEC.md 6.5). Only the file's head is
// parsed. A file that cannot be read or parsed as far as its package
// clause, such as one no longer in the tree, is not generated.
func IsGeneratedFile(path string) bool {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.PackageClauseOnly|parser.ParseComments)
	if err != nil {
		return false
	}
	return ast.IsGenerated(f)
}
