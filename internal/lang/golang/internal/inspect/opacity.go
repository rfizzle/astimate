package inspect

import (
	"go/ast"
	"strconv"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// OpacityFlags holds the v1 flags that mark code a reader cannot follow
// from Go source alone: uses_cgo, uses_reflect and generated_files.
type OpacityFlags struct {
	// Cgo is uses_cgo: some non-test file imports "C".
	Cgo bool
	// Reflect is uses_reflect: some non-test file imports reflect or
	// unsafe, under any name, blank and dot imports included.
	Reflect bool
	// Generated is generated_files: the non-test files ast.IsGenerated
	// reports, which carry a comment line matching
	// ^// Code generated .* DO NOT EDIT\.$ before the package clause, Go's
	// generated-file convention.
	Generated int
}

// Opacity computes the opacity flags of p from the syntax of its non-test
// source files. It reads import specs, not p.Imports: cgo's "C" is never
// in p.Imports, and for a cgo package the type-checked trees are the
// files cgo generated, which import unsafe themselves;
// load.Module.SourceSyntax gives the files as written.
func Opacity(m *load.Module, p *packages.Package) OpacityFlags {
	var o OpacityFlags
	for _, f := range m.SourceSyntax(p) {
		if ast.IsGenerated(f) {
			o.Generated++
		}
		for _, spec := range f.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			switch path {
			case "C":
				o.Cgo = true
			case "reflect", "unsafe":
				o.Reflect = true
			}
		}
	}
	return o
}
