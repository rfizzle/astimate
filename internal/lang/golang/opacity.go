package golang

import (
	"go/ast"
	"strconv"

	"golang.org/x/tools/go/packages"
)

// opacityFlags holds the v1 flags that mark code a reader cannot follow
// from Go source alone: uses_cgo, uses_reflect and generated_files.
type opacityFlags struct {
	// cgo is uses_cgo: some non-test file imports "C".
	cgo bool
	// reflect is uses_reflect: some non-test file imports reflect or
	// unsafe, under any name, blank and dot imports included.
	reflect bool
	// generated is generated_files: the non-test files ast.IsGenerated
	// reports, which carry a comment line matching
	// ^// Code generated .* DO NOT EDIT\.$ before the package clause, Go's
	// generated-file convention.
	generated int
}

// opacity computes the opacity flags of p from the syntax of its non-test
// source files. It reads import specs, not p.Imports: cgo's "C" is never
// in p.Imports, and for a cgo package the type-checked trees are the
// files cgo generated, which import unsafe themselves; sourceSyntax gives
// the files as written.
func opacity(l *loaded, p *packages.Package) opacityFlags {
	var o opacityFlags
	for _, f := range sourceSyntax(l, p) {
		if ast.IsGenerated(f) {
			o.generated++
		}
		for _, spec := range f.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			switch path {
			case "C":
				o.cgo = true
			case "reflect", "unsafe":
				o.reflect = true
			}
		}
	}
	return o
}
