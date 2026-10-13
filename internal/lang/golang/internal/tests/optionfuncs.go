package tests

import "go/ast"

// isOptionFunc reports whether fd is a functional-option constructor
// (SPEC.md 6.4, rule a): a func or method with a body of one statement, a
// return of a func literal that has a parameter and consists of nothing but
// assignments to fields of that parameter. WithTimeout in
//
//	func WithTimeout(d time.Duration) Option {
//		return func(o *options) { o.timeout = d }
//	}
//
// is one. A literal that returns a value, validates, branches or has any
// other statement is more than a field setter and is not.
func isOptionFunc(fd *ast.FuncDecl) bool {
	if fd.Body == nil || len(fd.Body.List) != 1 {
		return false
	}
	ret, ok := fd.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	lit, ok := ret.Results[0].(*ast.FuncLit)
	if !ok || len(lit.Body.List) == 0 || lit.Type.Results != nil {
		return false
	}
	params := make(map[string]bool)
	for _, field := range lit.Type.Params.List {
		for _, name := range field.Names {
			if name.Name != "_" {
				params[name.Name] = true
			}
		}
	}
	if len(params) == 0 {
		return false
	}
	for _, stmt := range lit.Body.List {
		as, ok := stmt.(*ast.AssignStmt)
		if !ok {
			return false
		}
		for _, lhs := range as.Lhs {
			if !assignsParamField(lhs, params) {
				return false
			}
		}
	}
	return true
}

// assignsParamField reports whether e is a selector chain, such as o.a or
// o.a.b, rooted at one of the names in params.
func assignsParamField(e ast.Expr, params map[string]bool) bool {
	for {
		switch x := e.(type) {
		case *ast.SelectorExpr:
			e = x.X
			if id, ok := e.(*ast.Ident); ok {
				return params[id.Name]
			}
		case *ast.ParenExpr:
			e = x.X
		default:
			return false
		}
	}
}
