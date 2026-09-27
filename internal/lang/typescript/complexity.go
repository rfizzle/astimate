package typescript

import (
	"slices"

	sitter "github.com/odvcencio/gotreesitter"
)

// Cognitive complexity follows the gocognit rules the Go extractor uses
// (SonarSource's definition), mapped onto TypeScript syntax:
//
//   - +1 plus the nesting level for if, switch, for, for...in, for...of,
//     while, do, catch and the conditional (ternary) operator;
//   - +1 for else and for else if, without the nesting level;
//   - +1 for each run of like logical operators (&&, ||, ??) in a binary
//     expression tree, a parenthesized subexpression starting a new tree;
//   - +1 for break or continue with a label;
//   - the nesting level rises inside the bodies of if (not else), switch,
//     the loops, catch, the branches of a ternary, and nested functions,
//     arrow functions and methods.
//
// Recursion is not counted: without type information a call cannot be
// told to target the enclosing function. try and finally add nothing.
//
// max_nesting counts the depth of if, for, for...in, for...of, while, do,
// switch, try, and nested function, arrow function and method nodes, the
// function's own body being depth 0. An else if is an if inside the outer
// one, so it adds a level, as in the Go extractor; case clauses, catch and
// finally add nothing beyond their try or switch.

// complexity scores n, of type typ, inside a function when it is a node the
// rules above name, visiting its children itself, and reports whether it
// did.
func (w *walker) complexity(n *sitter.Node, typ string, st state) bool {
	switch typ {
	case "if_statement":
		w.ifStatement(n, st, false)
	case "for_statement", "for_in_statement", "while_statement", "do_statement", "switch_statement":
		w.fn.cognitive += st.nest + 1
		w.bodyNested(n, st, true)
	case "catch_clause":
		w.fn.cognitive += st.nest + 1
		w.bodyNested(n, st, false)
	case "try_statement":
		w.children(n, state{nest: st.nest, depth: st.depth + 1, quiet: st.quiet})
	case "ternary_expression", "conditional_expression":
		w.fn.cognitive += st.nest + 1
		w.fieldsNested(n, state{nest: st.nest, depth: st.depth, quiet: st.quiet}, "consequence", "alternative")
	case "arrow_function", "function_expression", "function", "generator_function",
		"function_declaration", "generator_function_declaration", "method_definition":
		w.children(n, state{nest: st.nest + 1, depth: st.depth + 1, quiet: st.quiet})
	case "break_statement", "continue_statement":
		if hasChildType(w, n, "statement_identifier") {
			w.fn.cognitive++
		}
		return false
	case "binary_expression":
		w.binary(n, st)
	default:
		return false
	}
	return true
}

// ifStatement scores an if statement: +1 plus the nesting level, or +1
// alone for the if of an else if. Its consequence is one level deeper; an
// else block is not, and scores +1.
func (w *walker) ifStatement(n *sitter.Node, st state, elseIf bool) {
	w.fn.nesting = max(w.fn.nesting, st.depth)
	if elseIf {
		w.fn.cognitive++
	} else {
		w.fn.cognitive += st.nest + 1
	}
	inner := state{nest: st.nest, depth: st.depth + 1, quiet: st.quiet}
	for i := range n.ChildCount() {
		c := n.Child(i)
		switch n.FieldNameForChild(i, w.lang) {
		case "consequence":
			w.visit(c, state{nest: st.nest + 1, depth: inner.depth, quiet: st.quiet})
		case "alternative":
			w.elseClause(c, inner)
		default:
			w.visit(c, inner)
		}
	}
}

// elseClause scores an else clause: an if inside it is an else if, and any
// other statement is an else block, +1, walked at the if's own nesting
// level as gocognit walks it.
func (w *walker) elseClause(n *sitter.Node, st state) {
	if n.Type(w.lang) != "else_clause" {
		w.visit(n, st)
		return
	}
	for i := range n.ChildCount() {
		c := n.Child(i)
		switch {
		case c.Type(w.lang) == "if_statement":
			w.ifStatement(c, st, true)
		case c.IsNamed() && c.Type(w.lang) != "comment":
			w.fn.cognitive++
			w.visit(c, st)
		default:
			w.visit(c, st)
		}
	}
}

// bodyNested walks n with its body one nesting level deeper and, when
// deepen is set, every child one max_nesting level deeper.
func (w *walker) bodyNested(n *sitter.Node, st state, deepen bool) {
	inner := state{nest: st.nest, depth: st.depth, quiet: st.quiet}
	if deepen {
		inner.depth++
	}
	w.fieldsNested(n, inner, "body")
}

// fieldsNested visits the children of n with flat, except those in one of
// fields, which it visits one nesting level deeper.
func (w *walker) fieldsNested(n *sitter.Node, flat state, fields ...string) {
	deep := flat
	deep.nest++
	for i := range n.ChildCount() {
		st := flat
		if slices.Contains(fields, n.FieldNameForChild(i, w.lang)) {
			st = deep
		}
		w.visit(n.Child(i), st)
	}
}

// binary scores a binary expression: the first one of a tree, if logical,
// adds one per run of like logical operators over the whole tree of binary
// expressions below it, which then count nothing themselves.
func (w *walker) binary(n *sitter.Node, st state) {
	counted := st.chained
	if !st.chained && isLogicalOp(w.operator(n)) {
		var last string
		for _, op := range w.logicalOps(n, nil) {
			if op != last {
				w.fn.cognitive++
				last = op
			}
		}
		counted = true
	}
	w.children(n, state{nest: st.nest, depth: st.depth, chained: counted, quiet: st.quiet})
}

// logicalOps appends the logical operators of the binary expression tree
// rooted at n to ops, left to right.
func (w *walker) logicalOps(n *sitter.Node, ops []string) []string {
	if n == nil || n.Type(w.lang) != "binary_expression" {
		return ops
	}
	ops = w.logicalOps(w.field(n, "left"), ops)
	if op := w.operator(n); isLogicalOp(op) {
		ops = append(ops, op)
	}
	return w.logicalOps(w.field(n, "right"), ops)
}

// operator returns the operator of binary expression n.
func (w *walker) operator(n *sitter.Node) string {
	if op := w.field(n, "operator"); op != nil {
		return op.Type(w.lang)
	}
	return ""
}

// isLogicalOp reports whether op is &&, || or ??.
func isLogicalOp(op string) bool {
	return op == "&&" || op == "||" || op == "??"
}

// p90 returns the nearest-rank 90th percentile of scores: the value at
// 1-based rank ceil(0.9 * n) after sorting ascending, or 0 when scores is
// empty. It sorts scores in place.
func p90(scores []int) int {
	n := len(scores)
	if n == 0 {
		return 0
	}
	slices.Sort(scores)
	return scores[(9*n+9)/10-1]
}
