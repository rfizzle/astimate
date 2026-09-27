package golang

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/packages"
)

// globalCounts is the hidden-contract state of one package: package-level
// variables and init functions, which no exported signature reveals.
type globalCounts struct {
	// globals is the number of names declared by package-level var specs,
	// excluding the blank identifier.
	globals int
	// initFuncs is the number of top-level func init() declarations.
	initFuncs int
	// exported and unexported are the counted global names, split by
	// visibility, in declaration order. They are a debug breakdown of globals.
	exported, unexported []string
}

// globals counts the package-level variables and init functions declared in
// p's non-test files. It reads only the top-level declarations of each file,
// so variables declared inside function bodies never count. Constants are not
// state and are ignored. A var spec counts each of its names, so
// var a, b = 1, 2 is 2.
func globals(_ *loaded, p *packages.Package) globalCounts {
	var c globalCounts
	for _, f := range p.Syntax {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				if d.Tok == token.VAR {
					c.addVars(d)
				}
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.Name == "init" {
					c.initFuncs++
				}
			}
		}
	}
	return c
}

// addVars counts every non-blank name declared by the specs of a var
// declaration.
func (c *globalCounts) addVars(d *ast.GenDecl) {
	for _, s := range d.Specs {
		vs, ok := s.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, n := range vs.Names {
			if n.Name == "_" {
				continue
			}
			c.globals++
			if n.IsExported() {
				c.exported = append(c.exported, n.Name)
			} else {
				c.unexported = append(c.unexported, n.Name)
			}
		}
	}
}
