package inspect

import (
	"go/ast"
	"go/token"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// GlobalCounts is the hidden-contract state of one package: package-level
// variables and init functions, which no exported signature reveals.
type GlobalCounts struct {
	// Globals is the number of names declared by package-level var specs,
	// excluding the blank identifier.
	Globals int
	// InitFuncs is the number of top-level func init() declarations.
	InitFuncs int
	// Exported and Unexported are the counted global names, split by
	// visibility, in declaration order. They are a debug breakdown of
	// Globals.
	Exported, Unexported []string
	// Pos holds the declaring identifier of each counted global, in
	// declaration order, for annotations.
	Pos []token.Pos
	// Names holds the name of each counted global, index for index with
	// Pos.
	Names []string
}

// Globals counts the package-level variables and init functions declared in
// p's authored non-test files; a generated file's state is its generator's
// (see load.Module.AuthoredSyntax). It reads only the top-level declarations
// of each file, so variables declared inside function bodies never count.
// Constants are not state and are ignored. A var spec counts each of its
// names, so var a, b = 1, 2 is 2.
func Globals(m *load.Module, p *packages.Package) GlobalCounts {
	var c GlobalCounts
	for d := range decls(m, p) {
		switch d := d.(type) {
		case *ast.GenDecl:
			if d.Tok == token.VAR {
				c.addVars(d)
			}
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == "init" {
				c.InitFuncs++
			}
		}
	}
	return c
}

// addVars counts every non-blank name declared by the specs of a var
// declaration.
func (c *GlobalCounts) addVars(d *ast.GenDecl) {
	for _, s := range d.Specs {
		vs, ok := s.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, n := range vs.Names {
			if n.Name == "_" {
				continue
			}
			c.Globals++
			c.Pos = append(c.Pos, n.Pos())
			c.Names = append(c.Names, n.Name)
			if n.IsExported() {
				c.Exported = append(c.Exported, n.Name)
			} else {
				c.Unexported = append(c.Unexported, n.Name)
			}
		}
	}
}
