package dup

// Token shapes for the duplicate block kinds (SPEC.md 6.3, duptok's
// kind.go). The syntax tree of each file is walked once into spans, byte
// ranges that each give the tokens in them a duptok.Shape; spans nest, and
// a token takes the shape of the innermost span holding it, ShapeNeutral
// outside every span.
//
//   - A composite literal, a struct type, and a const or var declaration,
//     at package level or in a function, are ShapeDecl.
//   - A function declaration or function literal is ShapeNeutral, so its
//     header and the braces of its body are, and its statements take their
//     own shapes; a function literal inside a declaration or an idiom
//     statement resets its body the same way.
//   - An idiom statement is ShapeIdiom, nested statements included: an if
//     with no else whose condition is an identifier compared != nil and
//     whose body is one return statement, its init statement, when it has
//     one, an assignment of a call; a statement that is a call; and an
//     assignment (= or :=) whose every right-hand side is a call, a
//     composite literal or the address of one. In a file main.go of
//     package main a call statement is ShapeDecl instead: the registration
//     calls of a command's scaffolding.
//   - Any other statement is ShapeCode, its nested statements taking their
//     own shapes. A block, labeled, empty or declaration statement adds no
//     span of its own.

import (
	"go/ast"
	"go/token"
	"path/filepath"

	"github.com/rfizzle/astimate/internal/lang/duptok"
)

// span gives the tokens in the byte range [start, end) of a file a shape.
type span struct {
	start, end int
	shape      duptok.Shape
}

// spanWalker collects the spans of one file in pre-order, a span before
// the spans inside it, which is also the order of their starts.
type spanWalker struct {
	tf *token.File
	// main reports a file main.go of package main.
	main  bool
	spans *[]span
	// idiom reports that the walk is inside an idiom statement, whose
	// nested statements add no span.
	idiom bool
}

// shapeSpans returns the spans of f, positioned in tf, in pre-order.
func shapeSpans(tf *token.File, f *ast.File) []span {
	var spans []span
	main := f.Name != nil && f.Name.Name == "main" && filepath.Base(tf.Name()) == "main.go"
	ast.Walk(spanWalker{tf: tf, main: main, spans: &spans}, f)
	return spans
}

// add records a span over n with shape.
func (w spanWalker) add(n ast.Node, shape duptok.Shape) {
	*w.spans = append(*w.spans, span{start: w.tf.Offset(n.Pos()), end: w.tf.Offset(n.End()), shape: shape})
}

// Visit records the span n opens, if any, and returns the walker for n's
// children.
func (w spanWalker) Visit(n ast.Node) ast.Visitor {
	switch n := n.(type) {
	case nil:
		return nil
	case *ast.FuncDecl, *ast.FuncLit:
		w.add(n, duptok.ShapeNeutral)
		w.idiom = false
		return w
	case *ast.CompositeLit, *ast.StructType:
		w.add(n, duptok.ShapeDecl)
		return w
	case *ast.GenDecl:
		if n.Tok == token.CONST || n.Tok == token.VAR {
			w.add(n, duptok.ShapeDecl)
		}
		return w
	case ast.Stmt:
		if w.idiom {
			return w
		}
		if shape, ok := w.stmtShape(n); ok {
			w.add(n, shape)
			w.idiom = shape != duptok.ShapeCode
		}
	}
	return w
}

// stmtShape returns the shape of statement n and true, or false for a
// statement that adds no span.
func (w spanWalker) stmtShape(n ast.Stmt) (duptok.Shape, bool) {
	switch n := n.(type) {
	case *ast.BlockStmt, *ast.LabeledStmt, *ast.EmptyStmt, *ast.DeclStmt:
		return 0, false
	case *ast.ExprStmt:
		if isCall(n.X) {
			if w.main {
				return duptok.ShapeDecl, true
			}
			return duptok.ShapeIdiom, true
		}
	case *ast.AssignStmt:
		if isCallAssign(n, true) {
			return duptok.ShapeIdiom, true
		}
	case *ast.IfStmt:
		if isErrorReturn(n) {
			return duptok.ShapeIdiom, true
		}
	}
	return duptok.ShapeCode, true
}

// isCall reports whether e, parentheses aside, is a call.
func isCall(e ast.Expr) bool {
	_, ok := ast.Unparen(e).(*ast.CallExpr)
	return ok
}

// isCallAssign reports whether n is an = or := assignment whose every
// right-hand side is a call or, with literals, a composite literal or the
// address of one.
func isCallAssign(n *ast.AssignStmt, literals bool) bool {
	if n.Tok != token.ASSIGN && n.Tok != token.DEFINE {
		return false
	}
	for _, e := range n.Rhs {
		e = ast.Unparen(e)
		if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
			e = u.X
		}
		_, lit := e.(*ast.CompositeLit)
		if !isCall(e) && (!literals || !lit) {
			return false
		}
	}
	return true
}

// isErrorReturn reports whether n is if [x := f();] err != nil { return
// ... } with no else.
func isErrorReturn(n *ast.IfStmt) bool {
	if n.Else != nil || len(n.Body.List) != 1 {
		return false
	}
	if _, ok := n.Body.List[0].(*ast.ReturnStmt); !ok {
		return false
	}
	if n.Init != nil {
		a, ok := n.Init.(*ast.AssignStmt)
		if !ok || !isCallAssign(a, false) {
			return false
		}
	}
	c, ok := ast.Unparen(n.Cond).(*ast.BinaryExpr)
	if !ok || c.Op != token.NEQ {
		return false
	}
	_, id := ast.Unparen(c.X).(*ast.Ident)
	nilY, ok := ast.Unparen(c.Y).(*ast.Ident)
	return id && ok && nilY.Name == "nil"
}

// shapes hands out the shape of each token of one file in offset order.
type shapes struct {
	spans []span
	next  int
	open  []span
}

// at returns the shape of the token at byte offset off, which must not be
// below the offset of the previous call.
func (s *shapes) at(off int) duptok.Shape {
	for {
		for n := len(s.open); n > 0 && s.open[n-1].end <= off; n = len(s.open) {
			s.open = s.open[:n-1]
		}
		if s.next >= len(s.spans) || s.spans[s.next].start > off {
			break
		}
		if sp := s.spans[s.next]; sp.end > off {
			s.open = append(s.open, sp)
		}
		s.next++
	}
	if n := len(s.open); n > 0 {
		return s.open[n-1].shape
	}
	return duptok.ShapeNeutral
}
