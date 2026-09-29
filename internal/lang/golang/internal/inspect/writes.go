package inspect

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"
)

// writes records the package-level variables that a package's code
// assigns, increments, decrements or takes the address of, for the globals
// exclusions (SPEC.md 6.5). inspectBody feeds it the writing nodes of every
// function body, so the scan rides the walk that measures nesting; it walks
// package-level var initializers itself, as an ast.Visitor.
type writes struct {
	// info resolves identifiers of p.Syntax; nil without type information.
	info *types.Info
	// scope is the package scope; nil without type information.
	scope *types.Scope
	// names holds each recorded name.
	names map[string]bool
}

// newWrites returns an empty record for p.
func newWrites(p *packages.Package) *writes {
	w := &writes{info: p.TypesInfo}
	if p.Types != nil {
		w.scope = p.Types.Scope()
	}
	return w
}

// note records the variables n writes when n is an assignment (not a
// short variable declaration), an increment or decrement, an address-of
// expression or a range statement assigning to existing variables. A nil
// w records nothing.
func (w *writes) note(n ast.Node) {
	if w == nil {
		return
	}
	switch n := n.(type) {
	case *ast.AssignStmt:
		if n.Tok != token.DEFINE {
			for _, l := range n.Lhs {
				w.target(l)
			}
		}
	case *ast.IncDecStmt:
		w.target(n.X)
	case *ast.UnaryExpr:
		if n.Op == token.AND {
			w.target(n.X)
		}
	case *ast.RangeStmt:
		if n.Tok == token.ASSIGN {
			w.target(n.Key)
			w.target(n.Value)
		}
	}
}

// noteInitializers records the writes in the initializers of d when d is
// a package-level var declaration, such as a function literal assigning a
// variable or an address taken: var p = &version. A basic literal writes
// nothing and is not walked.
func (w *writes) noteInitializers(d ast.Decl) {
	gd, ok := d.(*ast.GenDecl)
	if !ok || gd.Tok != token.VAR {
		return
	}
	for _, s := range gd.Specs {
		vs, ok := s.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, v := range vs.Values {
			if _, ok := v.(*ast.BasicLit); ok {
				continue
			}
			ast.Walk(w, v)
		}
	}
}

// Visit records the writes of n and continues into its children.
func (w *writes) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		return nil
	}
	w.note(n)
	return w
}

// target records e when it is a plain identifier of a package-level
// variable. An identifier the type information does not resolve, as in a
// cgo package's source trees or a package without type information, is
// recorded by name whatever it denotes, which can only count more globals.
func (w *writes) target(e ast.Expr) {
	id, ok := e.(*ast.Ident)
	if !ok {
		if _, paren := e.(*ast.ParenExpr); !paren {
			return
		}
		if id, ok = ast.Unparen(e).(*ast.Ident); !ok {
			return
		}
	}
	if id.Name == "_" {
		return
	}
	if w.info != nil {
		if obj, ok := w.info.Uses[id]; ok {
			if v, ok := obj.(*types.Var); !ok || w.scope == nil || v.Parent() != w.scope {
				return
			}
		}
	}
	if w.names == nil {
		w.names = make(map[string]bool)
	}
	w.names[id.Name] = true
}
