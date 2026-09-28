package inspect

import (
	"slices"
	"strings"

	sitter "github.com/odvcencio/gotreesitter"
	"github.com/rfizzle/astimate/internal/lang/typescript/internal/walk"
)

// walker gathers the facts of one file in a single walk of its tree. The
// top-level statements go through statement, which records declarations
// and enters functions; everything below goes through the embedded
// walk.Walker, which emits duplication tokens, collects comments, imports
// and test identifiers, and scores the complexity of the function it is
// inside.
type walker struct {
	walk.Walker
	lang *sitter.Language
	f    *Facts

	// localFuncs maps the name of each top-level function to whether it
	// carries the untested directive and the line it is declared on, and
	// classMethods holds the untested_exports candidates of each top-level
	// class, for resolving export lists.
	localFuncs   map[string]localFunc
	classMethods map[string][]ExportedFunc
	// overloads holds the names of top-level functions with an overload
	// signature that carries the untested directive, which applies to the
	// implementation.
	overloads map[string]bool
	// listed holds the local and exported names of export { ... } lists
	// without a source, resolved once the whole file is walked.
	listed [][2]string
}

// localFunc is a top-level function an export list may name: whether it
// carries the untested directive, and the 1-based line of its declaration.
type localFunc struct {
	directed bool
	line     int
}

// program walks the top-level statements of the file rooted at root.
func (w *walker) program(root *sitter.Node) {
	w.localFuncs = map[string]localFunc{}
	w.classMethods = map[string][]ExportedFunc{}
	w.overloads = map[string]bool{}
	for i := range root.ChildCount() {
		w.statement(root.Child(i), false, w.hasDirective(root, i))
	}
	for _, l := range w.listed {
		local, exported := l[0], l[1]
		if lf, ok := w.localFuncs[local]; ok {
			w.f.ExportedFuncs = append(w.f.ExportedFuncs, ExportedFunc{Match: exported, Display: exported, Directed: lf.directed, Line: lf.line})
		}
		w.f.ExportedFuncs = append(w.f.ExportedFuncs, w.classMethods[local]...)
	}
	// A function exported under two names, or twice under one, is one
	// candidate per name.
	seen := make(map[ExportedFunc]bool, len(w.f.ExportedFuncs))
	w.f.ExportedFuncs = slices.DeleteFunc(w.f.ExportedFuncs, func(e ExportedFunc) bool {
		dup := seen[e]
		seen[e] = true
		return dup
	})
}

// statement records the declarations of the top-level statement n, which
// is exported when it is the declaration of an export statement and
// directed when the untested directive is in its doc comment, and walks
// it.
func (w *walker) statement(n *sitter.Node, exported, directed bool) {
	switch typ := n.Type(w.lang); typ {
	case "export_statement":
		w.exportStatement(n, directed)
	case "function_signature":
		// An overload signature: the directive in its doc comment applies
		// to the implementation that follows it.
		if directed {
			w.overloads[w.Text(w.Field(n, "name"))] = true
		}
		w.Visit(n)
	case "function_declaration", "generator_function_declaration":
		name := w.Text(w.Field(n, "name"))
		w.declareFunc(name, lineOf(n), exported, directed || w.overloads[name])
		w.Function("", name, n)
	case "class_declaration", "abstract_class_declaration", "class":
		w.class(n, exported)
	case "lexical_declaration", "variable_declaration":
		w.variables(n, typ == "variable_declaration" || w.Text(n.Child(0)) == "let", exported, directed)
	case "expression_statement":
		if w.f.Test {
			w.f.TestFuncs += w.testCases(n)
		} else if isCallStatement(w, n) {
			w.f.HasInit = true
		}
		w.Visit(n)
	default:
		w.Visit(n)
	}
}

// exportStatement counts the names an export statement adds, records its
// source as an import when it re-exports, and walks it. directed reports
// the untested directive in its doc comment, which applies to the
// functions it declares.
func (w *walker) exportStatement(n *sitter.Node, directed bool) {
	decl := w.Field(n, "declaration")
	value := w.Field(n, "value")
	if source := w.Field(n, "source"); source != nil {
		spec := walk.TrimQuotes(w.Text(source))
		w.Imports = append(w.Imports, spec)
		w.f.Reexports = append(w.f.Reexports, Reexport{Spec: spec, Names: w.exportedNames(n, false)})
	} else {
		switch {
		case decl != nil:
			w.countDeclaration(decl)
		case value != nil || w.HasChild(n, "default"):
			w.f.Exports++
			if value != nil {
				switch value.Type(w.lang) {
				case "identifier":
					w.listed = append(w.listed, [2]string{w.Text(value), w.Text(value)})
				case "class":
					w.f.ExportedTypes++
				}
			}
		default:
			if names := w.exportedNames(n, true); names > 0 || w.HasChild(n, "export_clause") {
				w.f.Exports += names
			} else {
				// export = x, export as namespace X.
				w.f.Exports++
			}
		}
	}
	for i := range n.ChildCount() {
		c := n.Child(i)
		switch {
		case decl != nil && sameNode(c, decl):
			w.statement(c, true, directed)
		case value != nil && sameNode(c, value) && isFunctionType(c.Type(w.lang)):
			name := w.Text(w.Field(c, "name"))
			if name != "" {
				w.f.ExportedFuncs = append(w.f.ExportedFuncs, ExportedFunc{Match: name, Display: name, Directed: directed || w.overloads[name], Line: lineOf(c)})
			} else {
				name = "default"
			}
			w.Function("", name, c)
		case value != nil && sameNode(c, value) && c.Type(w.lang) == "class":
			w.class(c, true)
		default:
			w.Visit(c)
		}
	}
}

// exportedNames counts the names in the export clause or namespace export
// of n. With local set it also records each specifier's local and exported
// names for resolving against the file's functions and classes.
func (w *walker) exportedNames(n *sitter.Node, local bool) int {
	names := 0
	for i := range n.NamedChildCount() {
		c := n.NamedChild(i)
		switch c.Type(w.lang) {
		case "namespace_export":
			names++
		case "export_clause":
			for j := range c.NamedChildCount() {
				s := c.NamedChild(j)
				if s.Type(w.lang) != "export_specifier" {
					continue
				}
				names++
				if local {
					name := w.Text(w.Field(s, "name"))
					exported := name
					if a := w.Field(s, "alias"); a != nil {
						exported = w.Text(a)
					}
					w.listed = append(w.listed, [2]string{name, exported})
				}
			}
		}
	}
	return names
}

// countDeclaration adds the exported names and types the exported
// declaration decl declares.
func (w *walker) countDeclaration(decl *sitter.Node) {
	switch decl.Type(w.lang) {
	case "function_signature":
		// An overload signature; the implementation is counted.
	case "class_declaration", "abstract_class_declaration", "type_alias_declaration", "enum_declaration":
		w.f.Exports++
		w.f.ExportedTypes++
	case "interface_declaration":
		w.f.Exports++
		w.f.ExportedTypes++
		w.f.ExportedInterfaces++
	case "lexical_declaration", "variable_declaration":
		for i := range decl.NamedChildCount() {
			if d := decl.NamedChild(i); d.Type(w.lang) == "variable_declarator" {
				w.f.Exports += len(w.bindings(w.Field(d, "name"), nil))
			}
		}
	default:
		w.f.Exports++
	}
}

// variables records a top-level let, const or var declaration: the names a
// let or var binds count as globals (mutable, excluding _), and a variable
// initialized with a function or arrow function is a function, directed
// when the declaration carries the untested directive.
func (w *walker) variables(n *sitter.Node, mutable, exported, directed bool) {
	for i := range n.ChildCount() {
		c := n.Child(i)
		if c.Type(w.lang) != "variable_declarator" {
			w.Visit(c)
			continue
		}
		nameNode, value := w.Field(c, "name"), w.Field(c, "value")
		if mutable {
			for _, b := range w.bindings(nameNode, nil) {
				if w.Text(b) != "_" {
					w.f.Globals = append(w.f.Globals, lineOf(b))
					w.f.GlobalNames = append(w.f.GlobalNames, w.Text(b))
				}
			}
		}
		if value == nil || !isFunctionType(value.Type(w.lang)) || nameNode.Type(w.lang) != "identifier" {
			w.Visit(c)
			continue
		}
		name := w.Text(nameNode)
		w.declareFunc(name, lineOf(c), exported, directed)
		w.functionChild("", name, c, value)
	}
}

// functionChild walks the children of parent, scoring its child fn as the
// function ident of class receiver, or the plain function ident when
// receiver is empty, and visiting the others.
func (w *walker) functionChild(receiver, ident string, parent, fn *sitter.Node) {
	for i := range parent.ChildCount() {
		if c := parent.Child(i); sameNode(c, fn) {
			w.Function(receiver, ident, c)
		} else {
			w.Visit(c)
		}
	}
}

// declareFunc records the top-level function name declared on line, for
// export lists to name, and as a candidate for untested_exports when it is
// exported; directed reports the untested directive.
func (w *walker) declareFunc(name string, line int, exported, directed bool) {
	w.localFuncs[name] = localFunc{directed: directed, line: line}
	if exported {
		w.f.ExportedFuncs = append(w.f.ExportedFuncs, ExportedFunc{Match: name, Display: name, Directed: directed, Line: line})
	}
}

// bindings appends to out the name nodes a binding pattern binds and
// returns the result: an identifier binds itself, and object and array
// patterns bind the names inside them, not their property keys or default
// values.
func (w *walker) bindings(n *sitter.Node, out []*sitter.Node) []*sitter.Node {
	if n == nil {
		return out
	}
	switch n.Type(w.lang) {
	case "identifier", "shorthand_property_identifier_pattern":
		return append(out, n)
	case "pair_pattern":
		return w.bindings(w.Field(n, "value"), out)
	case "assignment_pattern", "object_assignment_pattern":
		return w.bindings(w.Field(n, "left"), out)
	}
	for i := range n.NamedChildCount() {
		out = w.bindings(n.NamedChild(i), out)
	}
	return out
}

// lineOf returns the 1-based line n starts on.
func lineOf(n *sitter.Node) int {
	return int(n.StartPoint().Row) + 1
}

// class walks a top-level class, scoring each method and function-valued
// field as a function with the class name as its receiver. Its public
// methods, other than the constructor and accessors, are candidates for
// untested_exports when the class is exported, directly or through an
// export list.
func (w *walker) class(n *sitter.Node, exported bool) {
	cname := w.Text(w.Field(n, "name"))
	if cname == "" {
		cname = "default"
	}
	var candidates []ExportedFunc
	for i := range n.ChildCount() {
		body := n.Child(i)
		if body.Type(w.lang) == "class_body" {
			candidates = append(candidates, w.classBody(cname, body)...)
		} else {
			w.Visit(body)
		}
	}
	if exported {
		w.f.ExportedFuncs = append(w.f.ExportedFuncs, candidates...)
	}
	w.classMethods[cname] = candidates
}

// classBody walks the body of class cname, scoring its members, and returns
// its candidates for untested_exports. The untested directive on an
// overload signature of a method applies to the method.
func (w *walker) classBody(cname string, body *sitter.Node) []ExportedFunc {
	var (
		candidates []ExportedFunc
		overloads  map[string]bool
	)
	for j := range body.ChildCount() {
		m := body.Child(j)
		name, fn, candidate := w.member(m)
		if fn == nil {
			if m.Type(w.lang) == "method_signature" && w.hasDirective(body, j) {
				if overloads == nil {
					overloads = map[string]bool{}
				}
				overloads[name] = true
			}
			w.Visit(m)
			continue
		}
		if candidate {
			candidates = append(candidates, ExportedFunc{Match: name, Display: cname + "." + name, Directed: w.hasDirective(body, j) || overloads[name], Line: lineOf(m)})
		}
		if sameNode(fn, m) {
			w.Function(cname, name, m)
		} else {
			w.functionChild(cname, name, m, fn)
		}
	}
	return candidates
}

// member returns the name of the class member m and the node to score as
// its function: m itself for a method with a body, the value for a field
// initialized with a function or arrow function, nil otherwise. candidate
// reports a public, non-constructor, non-accessor member.
func (w *walker) member(m *sitter.Node) (name string, fn *sitter.Node, candidate bool) {
	nameNode := w.Field(m, "name")
	name = w.Text(nameNode)
	switch m.Type(w.lang) {
	case "method_definition":
		if w.Field(m, "body") == nil {
			return name, nil, false
		}
		fn = m
	case "public_field_definition":
		v := w.Field(m, "value")
		if v == nil || !isFunctionType(v.Type(w.lang)) {
			return name, nil, false
		}
		fn = v
	default:
		return name, nil, false
	}
	candidate = name != "constructor" && nameNode != nil && nameNode.Type(w.lang) != "private_property_identifier"
	for i := range m.ChildCount() {
		c := m.Child(i)
		switch c.Type(w.lang) {
		case "accessibility_modifier":
			if t := w.Text(c); t == "private" || t == "protected" {
				candidate = false
			}
		case "get", "set":
			candidate = false
		}
	}
	return name, fn, candidate
}

// untestedDirective marks an exported function or public method as
// intentionally untested when it is a line comment of the declaration's
// doc comment, alone or followed by a space and a reason, as in Go.
const untestedDirective = "//astimate:untested"

// hasDirective reports whether the doc comment of parent's i-th child holds
// the untested directive. The doc comment is the run of comments directly
// above the child, each ending on the line before the next begins, as Go
// groups them; a comment on the same line as the code before it belongs to
// that code and ends the run.
func (w *walker) hasDirective(parent *sitter.Node, i int) bool {
	next := parent.Child(i).StartPoint().Row
	for k := i - 1; k >= 0; k-- {
		c := parent.Child(k)
		if c.Type(w.lang) != "comment" || next > c.EndPoint().Row+1 {
			return false
		}
		if k > 0 {
			if prev := parent.Child(k - 1); prev.Type(w.lang) != "comment" && prev.EndPoint().Row == c.StartPoint().Row {
				return false
			}
		}
		rest, ok := strings.CutPrefix(strings.TrimRight(w.Text(c), "\r"), untestedDirective)
		if ok && (rest == "" || rest[0] == ' ') {
			return true
		}
		next = c.StartPoint().Row
	}
	return false
}

// testCases counts the test cases the top-level or describe-nested
// expression statement stmt declares: a call to it, test or bench (or a
// member or curried form of them, such as it.only or it.each(table)) whose
// first argument is a string or template literal counts once, and a call to
// describe in any form counts the test cases in the bodies of its function
// arguments.
func (w *walker) testCases(stmt *sitter.Node) int {
	if stmt.NamedChildCount() == 0 {
		return 0
	}
	call := stmt.NamedChild(0)
	if call.Type(w.lang) != "call_expression" {
		return 0
	}
	args := w.Field(call, "arguments")
	if args == nil {
		return 0
	}
	base := w.calleeBase(w.Field(call, "function"))
	switch {
	case isTestCallee(base):
		if first := firstArgument(w, args); first != nil && isStringNode(first.Type(w.lang)) {
			return 1
		}
	case base == "describe":
		return w.describeCases(args)
	}
	return 0
}

// describeCases counts the test cases in the bodies of the function
// arguments of a describe call, whose arguments node is args.
func (w *walker) describeCases(args *sitter.Node) int {
	n := 0
	for i := range args.NamedChildCount() {
		a := args.NamedChild(i)
		if !isFunctionType(a.Type(w.lang)) {
			continue
		}
		body := w.Field(a, "body")
		if body == nil || body.Type(w.lang) != "statement_block" {
			continue
		}
		for j := range body.NamedChildCount() {
			if s := body.NamedChild(j); s.Type(w.lang) == "expression_statement" {
				n += w.testCases(s)
			}
		}
	}
	return n
}

// calleeBase returns the identifier a callee expression starts from:
// it for it, it.only and it.each(table), "" for anything else.
func (w *walker) calleeBase(fn *sitter.Node) string {
	for fn != nil {
		switch fn.Type(w.lang) {
		case "identifier":
			return w.Text(fn)
		case "member_expression":
			fn = w.Field(fn, "object")
		case "call_expression":
			fn = w.Field(fn, "function")
		default:
			return ""
		}
	}
	return ""
}

// isCallStatement reports whether the expression statement n is a call,
// possibly awaited: module initialization code run on import.
func isCallStatement(w *walker, n *sitter.Node) bool {
	if n.NamedChildCount() == 0 {
		return false
	}
	e := n.NamedChild(0)
	if e.Type(w.lang) == "await_expression" && e.NamedChildCount() > 0 {
		e = e.NamedChild(0)
	}
	return e.Type(w.lang) == "call_expression"
}

// firstArgument returns the first argument in an arguments node, skipping
// comments.
func firstArgument(w *walker, args *sitter.Node) *sitter.Node {
	for i := range args.NamedChildCount() {
		if a := args.NamedChild(i); a.Type(w.lang) != "comment" {
			return a
		}
	}
	return nil
}

// sameNode reports whether a and b are the same node of the tree, by
// position and symbol, since node values may be materialized afresh.
func sameNode(a, b *sitter.Node) bool {
	return a.StartByte() == b.StartByte() && a.EndByte() == b.EndByte() && a.Symbol() == b.Symbol()
}

// isFunctionType reports whether a node of type typ is a function
// expression, arrow function or function declaration.
func isFunctionType(typ string) bool {
	switch typ {
	case "arrow_function", "function_expression", "function", "generator_function",
		"function_declaration", "generator_function_declaration":
		return true
	}
	return false
}
