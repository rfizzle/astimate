package walk

import (
	"strings"

	sitter "github.com/odvcencio/gotreesitter"
	"github.com/rfizzle/astimate/internal/lang/duptok"
)

// Token shapes for the duplicate block kinds (SPEC.md 6.3, duptok's
// kind.go), from node kinds. A node may open a shape that every token
// below it takes until a node further down opens another; tokens under no
// such node are duptok.ShapeNeutral.
//
//   - An object or array literal, an interface, type alias or enum
//     declaration, and a class field are ShapeDecl.
//   - A const, let or var declaration is ShapeDecl when every value is
//     absent, a literal, an object or array literal or a function; it is
//     ShapeIdiom when every value is one of those or a call, at least one a
//     call (await and new included); otherwise it is ShapeCode.
//   - A function, arrow function, method or class body is ShapeNeutral,
//     so its header and braces are, and its statements take their own
//     shapes.
//   - An idiom statement is ShapeIdiom, nested statements included: an
//     expression statement that is a call or an assignment of a call or of
//     an object or array literal, and an if with no else whose body is one
//     return or throw statement, the guard clause that is TypeScript's
//     error return.
//   - Any other statement is ShapeCode, its nested statements taking their
//     own shapes. A block, import, export or empty statement opens none.

// shaping is the shape a walk hands the tokens it meets.
type shaping struct {
	shape duptok.Shape
	// idiom reports the walk is inside an idiom statement, whose nested
	// statements open no shape of their own.
	idiom bool
}

// enter opens the shape node n of type typ gives its tokens, if any, and
// returns the shaping to restore after n.
func (w *Walker) enter(n *sitter.Node, typ string) shaping {
	prev := w.shaping
	switch {
	case resetsShape(typ):
		w.shaping = shaping{shape: duptok.ShapeNeutral}
	case isDeclNode(typ):
		w.shaping.shape = duptok.ShapeDecl
	case !w.shaping.idiom:
		if shape, ok := w.stmtShape(n, typ); ok {
			w.shaping = shaping{shape: shape, idiom: shape == duptok.ShapeIdiom}
		}
	}
	return prev
}

// resetsShape reports whether a node of type typ is a function, method or
// class body, whose statements take their own shapes.
func resetsShape(typ string) bool {
	switch typ {
	case "arrow_function", "function_expression", "function", "generator_function",
		"function_declaration", "generator_function_declaration", "method_definition", "class_body":
		return true
	}
	return false
}

// isDeclNode reports whether a node of type typ makes its tokens
// declarative: an object or array literal, a type declaration or a class
// field.
func isDeclNode(typ string) bool {
	switch typ {
	case "object", "array", "interface_declaration", "type_alias_declaration", "enum_declaration",
		"public_field_definition":
		return true
	}
	return false
}

// stmtShape returns the shape statement n of type typ opens and true, or
// false when n is no statement or a statement that opens none.
func (w *Walker) stmtShape(n *sitter.Node, typ string) (duptok.Shape, bool) {
	switch typ {
	case "lexical_declaration", "variable_declaration":
		return w.declShape(n), true
	case "expression_statement":
		return w.exprShape(n), true
	case "if_statement":
		return idiomIf(w.Field(n, "alternative") == nil && w.isGuardBody(w.Field(n, "consequence"))), true
	case "import_statement", "export_statement", "empty_statement":
		return 0, false
	}
	return duptok.ShapeCode, strings.HasSuffix(typ, "_statement")
}

// idiomIf returns ShapeIdiom when idiom holds, else ShapeCode.
func idiomIf(idiom bool) duptok.Shape {
	if idiom {
		return duptok.ShapeIdiom
	}
	return duptok.ShapeCode
}

// declShape returns the shape of a const, let or var declaration n.
func (w *Walker) declShape(n *sitter.Node) duptok.Shape {
	shape := duptok.ShapeDecl
	for i := range n.NamedChildCount() {
		d := n.NamedChild(i)
		if d.Type(w.lang) != "variable_declarator" {
			continue
		}
		switch v := w.unwrap(w.Field(d, "value")); {
		case v == nil || w.isData(v):
		case w.isCall(v):
			shape = duptok.ShapeIdiom
		default:
			return duptok.ShapeCode
		}
	}
	return shape
}

// exprShape returns the shape of expression statement n: idiom for a call
// or an assignment of a call or of an object or array literal.
func (w *Walker) exprShape(n *sitter.Node) duptok.Shape {
	e := w.unwrap(n.NamedChild(0))
	if e != nil && e.Type(w.lang) == "assignment_expression" {
		r := w.unwrap(w.Field(e, "right"))
		return idiomIf(r != nil && (w.isCall(r) || w.isLiteralData(r)))
	}
	return idiomIf(e != nil && w.isCall(e))
}

// isGuardBody reports whether the consequence c of an if is one return or
// throw statement, alone or in a block.
func (w *Walker) isGuardBody(c *sitter.Node) bool {
	if c != nil && c.Type(w.lang) == "statement_block" {
		c = w.onlyStatement(c)
	}
	if c == nil {
		return false
	}
	t := c.Type(w.lang)
	return t == "return_statement" || t == "throw_statement"
}

// onlyStatement returns the one statement of block b, comments aside, or
// nil when it holds none or several.
func (w *Walker) onlyStatement(b *sitter.Node) *sitter.Node {
	var only *sitter.Node
	for i := range b.NamedChildCount() {
		s := b.NamedChild(i)
		if s.Type(w.lang) == "comment" {
			continue
		}
		if only != nil {
			return nil
		}
		only = s
	}
	return only
}

// unwrap returns e without parentheses, type assertions, satisfies and
// non-null assertions around it; nil for nil.
func (w *Walker) unwrap(e *sitter.Node) *sitter.Node {
	for e != nil {
		switch e.Type(w.lang) {
		case "parenthesized_expression", "as_expression", "satisfies_expression", "non_null_expression":
			e = e.NamedChild(0)
		default:
			return e
		}
	}
	return nil
}

// isCall reports whether e is a call, a new expression or an await of
// one.
func (w *Walker) isCall(e *sitter.Node) bool {
	switch e.Type(w.lang) {
	case "call_expression", "new_expression":
		return true
	case "await_expression":
		a := w.unwrap(e.NamedChild(0))
		return a != nil && w.isCall(a)
	}
	return false
}

// isLiteralData reports whether e is an object or array literal.
func (w *Walker) isLiteralData(e *sitter.Node) bool {
	t := e.Type(w.lang)
	return t == "object" || t == "array"
}

// isData reports whether the declared value e holds no statement or
// operator of its own: a literal, an object or array literal, a function.
func (w *Walker) isData(e *sitter.Node) bool {
	if isLiteral(e, e.Type(w.lang)) || w.isLiteralData(e) {
		return true
	}
	switch e.Type(w.lang) {
	case "arrow_function", "function_expression", "function", "generator_function":
		return true
	}
	return false
}
