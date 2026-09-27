package typescript

import (
	"slices"
	"strings"

	sitter "github.com/odvcencio/gotreesitter"
	"github.com/rfizzle/astimate/internal/lang/duptok"
)

// funcScore is the complexity of one function: a top-level function
// declaration, a top-level variable initialized with a function or arrow
// function, or a method or function-valued field of a top-level class.
type funcScore struct {
	name string
	// cognitive is the cognitive complexity; nesting the deepest nesting
	// inside the body, which is itself depth 0.
	cognitive, nesting int
}

// walker gathers the facts of one file in a single walk of its tree. The
// top-level statements go through statement, which records declarations
// and enters functions; everything below goes through visit, which emits
// duplication tokens, collects comments, imports and test identifiers, and
// scores the complexity of the function it is inside.
type walker struct {
	s    *scanner
	lang *sitter.Language
	src  []byte
	f    *fileFacts

	comments []span
	// fn is the function being scored; nil outside functions.
	fn *funcScore
	// localFuncs names the top-level functions, and classMethods the
	// untested_exports candidates of each top-level class, for resolving
	// export lists.
	localFuncs   map[string]bool
	classMethods map[string][]exportedFunc
	// listed holds the local and exported names of export { ... } lists
	// without a source, resolved once the whole file is walked.
	listed [][2]string
	err    error
}

// state is the context visit passes down.
type state struct {
	// nest is the cognitive complexity nesting level; depth the max_nesting
	// depth. Both count from 0 at a function's body.
	nest, depth int
	// chained reports that the node's parent binary expression already
	// counted the logical operator sequence it belongs to.
	chained bool
	// quiet suppresses duplication tokens, inside a template literal that
	// was emitted as one literal token.
	quiet bool
}

// program walks the top-level statements of the file rooted at root.
func (w *walker) program(root *sitter.Node) {
	w.localFuncs = map[string]bool{}
	w.classMethods = map[string][]exportedFunc{}
	for i := range root.ChildCount() {
		w.statement(root.Child(i), false)
	}
	for _, l := range w.listed {
		local, exported := l[0], l[1]
		if w.localFuncs[local] {
			w.f.exportedFuncs = append(w.f.exportedFuncs, exportedFunc{match: exported, display: exported})
		}
		w.f.exportedFuncs = append(w.f.exportedFuncs, w.classMethods[local]...)
	}
	// A function exported under two names, or twice under one, is one
	// candidate per name.
	seen := make(map[exportedFunc]bool, len(w.f.exportedFuncs))
	w.f.exportedFuncs = slices.DeleteFunc(w.f.exportedFuncs, func(e exportedFunc) bool {
		dup := seen[e]
		seen[e] = true
		return dup
	})
}

// text returns the source text of n, or "" for nil.
func (w *walker) text(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	return n.Text(w.src)
}

// field returns the child of n in field name, or nil.
func (w *walker) field(n *sitter.Node, name string) *sitter.Node {
	return n.ChildByFieldName(name, w.lang)
}

// statement records the declarations of the top-level statement n, which
// is exported when it is the declaration of an export statement, and walks
// it.
func (w *walker) statement(n *sitter.Node, exported bool) {
	switch typ := n.Type(w.lang); typ {
	case "export_statement":
		w.exportStatement(n)
	case "function_declaration", "generator_function_declaration":
		name := w.text(w.field(n, "name"))
		w.localFuncs[name] = true
		if exported {
			w.f.exportedFuncs = append(w.f.exportedFuncs, exportedFunc{match: name, display: name})
		}
		w.function(name, n)
	case "class_declaration", "abstract_class_declaration", "class":
		w.class(n, exported)
	case "lexical_declaration", "variable_declaration":
		w.variables(n, typ == "variable_declaration" || w.text(n.Child(0)) == "let", exported)
	case "expression_statement":
		if w.f.test {
			w.f.testFuncs += w.testCases(n)
		} else if isCallStatement(w, n) {
			w.f.hasInit = true
		}
		w.visit(n, state{})
	default:
		w.visit(n, state{})
	}
}

// exportStatement counts the names an export statement adds, records its
// source as an import when it re-exports, and walks it.
func (w *walker) exportStatement(n *sitter.Node) {
	decl := w.field(n, "declaration")
	value := w.field(n, "value")
	if source := w.field(n, "source"); source != nil {
		spec := trimQuotes(w.text(source))
		w.f.imports = append(w.f.imports, spec)
		w.f.reexports = append(w.f.reexports, reexport{spec: spec, names: w.exportedNames(n, false)})
	} else {
		switch {
		case decl != nil:
			w.countDeclaration(decl)
		case value != nil || hasChildType(w, n, "default"):
			w.f.exports++
			if value != nil {
				switch value.Type(w.lang) {
				case "identifier":
					w.listed = append(w.listed, [2]string{w.text(value), w.text(value)})
				case "class":
					w.f.exportedTypes++
				}
			}
		default:
			if names := w.exportedNames(n, true); names > 0 || hasChildType(w, n, "export_clause") {
				w.f.exports += names
			} else {
				// export = x, export as namespace X.
				w.f.exports++
			}
		}
	}
	for i := range n.ChildCount() {
		c := n.Child(i)
		switch {
		case decl != nil && sameNode(c, decl):
			w.statement(c, true)
		case value != nil && sameNode(c, value) && isFunctionType(c.Type(w.lang)):
			name := w.text(w.field(c, "name"))
			if name != "" {
				w.f.exportedFuncs = append(w.f.exportedFuncs, exportedFunc{match: name, display: name})
			} else {
				name = "default"
			}
			w.function(name, c)
		case value != nil && sameNode(c, value) && c.Type(w.lang) == "class":
			w.class(c, true)
		default:
			w.visit(c, state{})
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
					name := w.text(w.field(s, "name"))
					exported := name
					if a := w.field(s, "alias"); a != nil {
						exported = w.text(a)
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
		w.f.exports++
		w.f.exportedTypes++
	case "interface_declaration":
		w.f.exports++
		w.f.exportedTypes++
		w.f.exportedInterfaces++
	case "lexical_declaration", "variable_declaration":
		for i := range decl.NamedChildCount() {
			if d := decl.NamedChild(i); d.Type(w.lang) == "variable_declarator" {
				w.f.exports += len(w.bindings(w.field(d, "name")))
			}
		}
	default:
		w.f.exports++
	}
}

// variables records a top-level let, const or var declaration: the names a
// let or var binds count as globals (mutable, excluding _), and a variable
// initialized with a function or arrow function is a function.
func (w *walker) variables(n *sitter.Node, mutable, exported bool) {
	for i := range n.ChildCount() {
		c := n.Child(i)
		if c.Type(w.lang) != "variable_declarator" {
			w.visit(c, state{})
			continue
		}
		nameNode, value := w.field(c, "name"), w.field(c, "value")
		if mutable {
			for _, b := range w.bindings(nameNode) {
				if b != "_" {
					w.f.globals++
				}
			}
		}
		if value == nil || !isFunctionType(value.Type(w.lang)) || nameNode.Type(w.lang) != "identifier" {
			w.visit(c, state{})
			continue
		}
		name := w.text(nameNode)
		w.localFuncs[name] = true
		if exported {
			w.f.exportedFuncs = append(w.f.exportedFuncs, exportedFunc{match: name, display: name})
		}
		for j := range c.ChildCount() {
			if d := c.Child(j); sameNode(d, value) {
				w.function(name, d)
			} else {
				w.visit(d, state{})
			}
		}
	}
}

// bindings returns the names a binding pattern binds: an identifier binds
// itself, and object and array patterns bind the names inside them, not
// their property keys or default values.
func (w *walker) bindings(n *sitter.Node) []string {
	if n == nil {
		return nil
	}
	switch n.Type(w.lang) {
	case "identifier", "shorthand_property_identifier_pattern":
		return []string{w.text(n)}
	case "pair_pattern":
		return w.bindings(w.field(n, "value"))
	case "assignment_pattern", "object_assignment_pattern":
		return w.bindings(w.field(n, "left"))
	}
	var out []string
	for i := range n.NamedChildCount() {
		out = append(out, w.bindings(n.NamedChild(i))...)
	}
	return out
}

// class walks a top-level class, scoring each method and function-valued
// field as a function named Class.member. Its public methods, other than
// the constructor and accessors, are candidates for untested_exports when
// the class is exported, directly or through an export list.
func (w *walker) class(n *sitter.Node, exported bool) {
	cname := w.text(w.field(n, "name"))
	if cname == "" {
		cname = "default"
	}
	var candidates []exportedFunc
	for i := range n.ChildCount() {
		body := n.Child(i)
		if body.Type(w.lang) == "class_body" {
			candidates = append(candidates, w.classBody(cname, body)...)
		} else {
			w.visit(body, state{})
		}
	}
	if exported {
		w.f.exportedFuncs = append(w.f.exportedFuncs, candidates...)
	}
	w.classMethods[cname] = candidates
}

// classBody walks the body of class cname, scoring its members, and returns
// its candidates for untested_exports.
func (w *walker) classBody(cname string, body *sitter.Node) []exportedFunc {
	var candidates []exportedFunc
	for j := range body.ChildCount() {
		m := body.Child(j)
		name, fn, candidate := w.member(m)
		if fn == nil {
			w.visit(m, state{})
			continue
		}
		if candidate {
			candidates = append(candidates, exportedFunc{match: name, display: cname + "." + name})
		}
		if sameNode(fn, m) {
			w.function(cname+"."+name, m)
			continue
		}
		for k := range m.ChildCount() {
			if c := m.Child(k); sameNode(c, fn) {
				w.function(cname+"."+name, c)
			} else {
				w.visit(c, state{})
			}
		}
	}
	return candidates
}

// member returns the name of the class member m and the node to score as
// its function: m itself for a method with a body, the value for a field
// initialized with a function or arrow function, nil otherwise. candidate
// reports a public, non-constructor, non-accessor member.
func (w *walker) member(m *sitter.Node) (name string, fn *sitter.Node, candidate bool) {
	nameNode := w.field(m, "name")
	name = w.text(nameNode)
	switch m.Type(w.lang) {
	case "method_definition":
		if w.field(m, "body") == nil {
			return name, nil, false
		}
		fn = m
	case "public_field_definition":
		v := w.field(m, "value")
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
			if t := w.text(c); t == "private" || t == "protected" {
				candidate = false
			}
		case "get", "set":
			candidate = false
		}
	}
	return name, fn, candidate
}

// function scores the function fn, named name, walking its children.
// Functions of test files are walked but not recorded.
func (w *walker) function(name string, fn *sitter.Node) {
	score := &funcScore{name: name}
	prev := w.fn
	w.fn = score
	for i := range fn.ChildCount() {
		w.visit(fn.Child(i), state{})
	}
	w.fn = prev
	if !w.f.test {
		w.f.funcs = append(w.f.funcs, *score)
	}
}

// visit walks n and everything below it.
func (w *walker) visit(n *sitter.Node, st state) {
	cc := n.ChildCount()
	if cc == 0 && n.StartByte() == n.EndByte() {
		// Zero-width tokens: automatic semicolons and missing nodes.
		return
	}
	typ := n.Type(w.lang)
	switch typ {
	case "comment", "html_comment", "hash_bang_line":
		w.comments = append(w.comments, span{int(n.StartByte()), int(n.EndByte())})
		return
	case "import_statement", "call_expression":
		w.importOf(n, typ)
	}
	if w.fn != nil {
		w.fn.nesting = max(w.fn.nesting, st.depth)
	}
	if cc == 0 || isLiteral(n, typ) {
		w.leaf(n, typ, st, false)
		if typ == "template_string" {
			quiet := st
			quiet.quiet, quiet.chained = true, false
			w.children(n, quiet)
		}
		return
	}
	if w.fn != nil && w.complexity(n, typ, st) {
		return
	}
	if typ == "unary_expression" && cc == 2 && isSignedNumber(w, n) {
		w.leaf(n.Child(0), w.text(n.Child(0)), st, true)
		w.visit(n.Child(1), state{nest: st.nest, depth: st.depth, quiet: st.quiet})
		return
	}
	w.children(n, state{nest: st.nest, depth: st.depth, quiet: st.quiet})
}

// children visits every child of n with st.
func (w *walker) children(n *sitter.Node, st state) {
	for i := range n.ChildCount() {
		w.visit(n.Child(i), st)
	}
}

// importOf records the specifier of an import declaration, an import =
// require(...) declaration, or a require(...) or import(...) call with a
// string literal argument.
func (w *walker) importOf(n *sitter.Node, typ string) {
	var spec *sitter.Node
	switch typ {
	case "import_statement":
		spec = w.field(n, "source")
		// import x = require("y") holds its source one level down.
		for i := 0; spec == nil && i < n.ChildCount(); i++ {
			spec = w.field(n.Child(i), "source")
		}
	case "call_expression":
		if !w.isModuleLoader(w.field(n, "function")) {
			return
		}
		args := w.field(n, "arguments")
		if args == nil || args.NamedChildCount() != 1 {
			return
		}
		if a := args.NamedChild(0); a.Type(w.lang) == "string" {
			spec = a
		}
	}
	if spec != nil {
		w.f.imports = append(w.f.imports, trimQuotes(w.text(spec)))
	}
}

// isModuleLoader reports whether the callee fn is import or require.
func (w *walker) isModuleLoader(fn *sitter.Node) bool {
	if fn == nil {
		return false
	}
	switch fn.Type(w.lang) {
	case "import":
		return true
	case "identifier":
		return w.text(fn) == "require"
	}
	return false
}

// leaf records a token: an identifier's text in a test file, and the
// normalized duplication token in a non-test file unless st is quiet. sign
// marks a unary + or - applied to a numeric literal.
func (w *walker) leaf(n *sitter.Node, typ string, st state, sign bool) {
	named := n.IsNamed()
	if w.f.test {
		if named && isIdentType(typ) {
			w.f.idents[w.text(n)] = true
		}
		return
	}
	if st.quiet {
		return
	}
	var (
		code  int32
		class = duptok.Code
	)
	switch {
	case isLiteral(n, typ):
		code, class = literalCode, duptok.Literal
	case named && (isIdentType(typ) || typ == "undefined"):
		code = identCode
	default:
		key := typ
		if named {
			key = "#" + typ
		}
		code = w.s.code(key)
		switch {
		case sign:
			class = duptok.Sign
		case !named && isTablePunct(typ):
			class = duptok.Punct
		}
	}
	t := &w.f.toks
	t.codes = append(t.codes, code)
	t.class = append(t.class, class)
	t.line = append(t.line, int32(n.StartPoint().Row)+1)
	t.last = append(t.last, int32(n.EndPoint().Row)+1)
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
	args := w.field(call, "arguments")
	if args == nil {
		return 0
	}
	base := w.calleeBase(w.field(call, "function"))
	switch {
	case isTestCallee(base):
		if first := firstArgument(w, args); first != nil && isStringNode(first.Type(w.lang)) {
			return 1
		}
	case base == "describe":
		n := 0
		for i := range args.NamedChildCount() {
			a := args.NamedChild(i)
			if !isFunctionType(a.Type(w.lang)) {
				continue
			}
			body := w.field(a, "body")
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
	return 0
}

// calleeBase returns the identifier a callee expression starts from:
// it for it, it.only and it.each(table), "" for anything else.
func (w *walker) calleeBase(fn *sitter.Node) string {
	for fn != nil {
		switch fn.Type(w.lang) {
		case "identifier":
			return w.text(fn)
		case "member_expression":
			fn = w.field(fn, "object")
		case "call_expression":
			fn = w.field(fn, "function")
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

// isSignedNumber reports whether the two-child unary expression n is a +
// or - applied to a numeric literal.
func isSignedNumber(w *walker, n *sitter.Node) bool {
	op, arg := n.Child(0), n.Child(1)
	t := op.Type(w.lang)
	return (t == "-" || t == "+") && arg.IsNamed() && arg.Type(w.lang) == "number"
}

// hasChildType reports whether n has a direct child of type typ.
func hasChildType(w *walker, n *sitter.Node, typ string) bool {
	for i := range n.ChildCount() {
		if n.Child(i).Type(w.lang) == typ {
			return true
		}
	}
	return false
}

// sameNode reports whether a and b are the same node of the tree, by
// position and symbol, since node values may be materialized afresh.
func sameNode(a, b *sitter.Node) bool {
	return a.StartByte() == b.StartByte() && a.EndByte() == b.EndByte() && a.Symbol() == b.Symbol()
}

// isLiteral reports whether n, of type typ, is a literal: a string,
// template, regular expression, number, true, false, null or JSX text.
// Keywords spelled like them, such as the string and number types, are
// anonymous and do not count.
func isLiteral(n *sitter.Node, typ string) bool {
	if !n.IsNamed() {
		return false
	}
	switch typ {
	case "string", "template_string", "regex", "number", "true", "false", "null", "jsx_text":
		return true
	}
	return false
}

// isTablePunct reports whether an anonymous token of type typ is
// punctuation a literal table is written with.
func isTablePunct(typ string) bool {
	return len(typ) == 1 && strings.IndexByte(",{}:[]();", typ[0]) >= 0
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
