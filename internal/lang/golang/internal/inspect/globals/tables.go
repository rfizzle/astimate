package globals

import (
	"go/ast"
	"go/token"
	"go/types"
)

// regexps reports whether every name of vs is a *regexp.Regexp initialized
// by regexp.MustCompile or regexp.MustCompilePOSIX of constant arguments
// (SPEC.md 6.5 (d)). A compiled pattern holds no state to change.
func (k *varKinds) regexps(vs *ast.ValueSpec) bool {
	return k.each(vs, func(_ string, t types.Type) bool { return isRegexp(t) }, k.compiled)
}

// each reports whether vs has one initializer per name, every non-blank
// name has a type typeOK accepts and every initializer satisfies value.
func (k *varKinds) each(vs *ast.ValueSpec, typeOK func(name string, t types.Type) bool, value func(ast.Expr) bool) bool {
	if k.scope == nil || len(vs.Values) != len(vs.Names) {
		return false
	}
	for i, n := range vs.Names {
		if n.Name != "_" {
			v, ok := k.scope.Lookup(n.Name).(*types.Var)
			if !ok || !typeOK(n.Name, v.Type()) {
				return false
			}
		}
		if !value(vs.Values[i]) {
			return false
		}
	}
	return true
}

// isRegexp reports whether t is *regexp.Regexp.
func isRegexp(t types.Type) bool {
	p, ok := types.Unalias(t).(*types.Pointer)
	if !ok {
		return false
	}
	n, ok := types.Unalias(p.Elem()).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "regexp" && n.Obj().Name() == "Regexp"
}

// compiled reports whether e is regexp.MustCompile or MustCompilePOSIX of
// constant arguments.
func (k *varKinds) compiled(e ast.Expr) bool {
	return k.constCall(e, func(path, name string) bool {
		return path == "regexp" && (name == "MustCompile" || name == "MustCompilePOSIX")
	})
}

// table reports whether every name of vs is a map, slice or array
// initialized by a non-empty composite literal of constants, nested literals and
// compiled regexps, and no code in the package indexes it on the left of
// an assignment or hands it to a call that could mutate it (SPEC.md 6.5
// (e)).
func (k *varKinds) table(vs *ast.ValueSpec) bool {
	kind := func(name string, t types.Type) bool {
		switch types.Unalias(t).Underlying().(type) {
		case *types.Map, *types.Slice, *types.Array:
			return !k.touched()[name]
		}
		return false
	}
	return k.each(vs, kind, func(e ast.Expr) bool {
		lit, ok := ast.Unparen(e).(*ast.CompositeLit)
		return ok && k.readOnly(lit, true)
	})
}

// readOnly reports whether lit has an element and every key and element of lit is a constant, a
// compiled regexp or a nested literal that is itself readOnly. A nested
// literal that may be a pointer (an elided &T{...}) holds state; without
// type information an elided literal is not accepted.
func (k *varKinds) readOnly(lit *ast.CompositeLit, top bool) bool {
	if len(lit.Elts) == 0 || (!top && k.pointerLit(lit)) {
		return false
	}
	for _, e := range lit.Elts {
		if kv, ok := e.(*ast.KeyValueExpr); ok {
			if !k.element(kv.Key) {
				return false
			}
			e = kv.Value
		}
		if !k.element(e) {
			return false
		}
	}
	return true
}

// element reports whether e is a constant, a compiled regexp or a
// readOnly literal.
func (k *varKinds) element(e ast.Expr) bool {
	if lit, ok := ast.Unparen(e).(*ast.CompositeLit); ok {
		return k.readOnly(lit, false)
	}
	return k.constant(e) || k.compiled(e)
}

// pointerLit reports whether lit denotes a pointer, or cannot be told not
// to: its type is elided and k has no type information for it.
func (k *varKinds) pointerLit(lit *ast.CompositeLit) bool {
	if k.info == nil {
		return lit.Type == nil
	}
	tv, ok := k.info.Types[lit]
	if !ok {
		return lit.Type == nil
	}
	_, ptr := tv.Type.Underlying().(*types.Pointer)
	return ptr
}

// touched returns, computing it once, the names of the package-level
// variables that code in the package could mutate through a reference: an
// element assigned, incremented, decremented or ranged into, or the
// variable passed to append, delete, clear, copy, a function of maps,
// slices or sort, a function or method of the package, or one the type
// information does not resolve, or used as the receiver of such a method.
// Reading it in an expression, a range, an index read, len or cap, or
// passing it to another package's function, does not.
func (k *varKinds) touched() map[string]bool {
	if k.touch != nil {
		return k.touch
	}
	k.touch = make(map[string]bool)
	for _, f := range k.files {
		ast.Inspect(f, func(n ast.Node) bool {
			k.noteTouch(n)
			return true
		})
	}
	return k.touch
}

// noteTouch records the variables n could mutate through an element.
func (k *varKinds) noteTouch(n ast.Node) {
	switch n := n.(type) {
	case *ast.AssignStmt:
		if n.Tok != token.DEFINE {
			k.reachAll(true, n.Lhs...)
		}
	case *ast.IncDecStmt:
		k.reach(n.X, true)
	case *ast.RangeStmt:
		if n.Tok == token.ASSIGN {
			k.reachAll(true, n.Key, n.Value)
		}
	case *ast.CallExpr:
		k.callTouch(n)
	}
}

// reachAll records each of es as reach does.
func (k *varKinds) reachAll(element bool, es ...ast.Expr) {
	for _, e := range es {
		k.reach(e, element)
	}
}

// callTouch records the variables call could mutate through its receiver
// or arguments.
func (k *varKinds) callTouch(call *ast.CallExpr) {
	path, _ := k.callee(call.Fun)
	switch path {
	case "":
		if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok && (id.Name == "len" || id.Name == "cap") {
			return
		}
	case k.pkgPath, "maps", "slices", "sort":
	default:
		return
	}
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		k.reach(sel.X, false)
	}
	k.reachAll(false, call.Args...)
}

// reach records the package-level variable at the root of e's index,
// slice, selector, dereference and parenthesis chain. With element set a
// bare identifier is not recorded, as the write scan reads it.
func (k *varKinds) reach(e ast.Expr, element bool) {
	var root *ast.Ident
	done := false
	ast.Inspect(e, func(n ast.Node) bool {
		if done || n == nil {
			return false
		}
		switch n := n.(type) {
		case *ast.ParenExpr, *ast.IndexExpr, *ast.SliceExpr, *ast.SelectorExpr, *ast.StarExpr:
			return true
		case *ast.Ident:
			root = n
		}
		done = true
		return false
	})
	if root != nil && root.Name != "_" && (!element || ast.Unparen(e) != root) && k.global(root) {
		k.touch[root.Name] = true
	}
}

// global reports whether id denotes a package-level variable, or cannot be
// told not to: the type information does not resolve it.
func (k *varKinds) global(id *ast.Ident) bool {
	if k.info == nil {
		return true
	}
	obj, ok := k.info.Uses[id]
	if !ok {
		return true
	}
	v, isVar := obj.(*types.Var)
	return isVar && v.Parent() == k.scope
}
