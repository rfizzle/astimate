// Package walk visits the syntax of one TypeScript file below its
// top-level statements, in the same single pass that records the file's
// declarations: it emits the normalized duplication tokens, collects
// comments, import specifiers and the identifiers of a test file, and
// scores each function it is handed for cognitive complexity and nesting
// while fingerprinting its body (SPEC.md 6.3, 6.5 and 13.1). The package
// inspect drives it; nothing here reads a file or keeps a tree.
package walk

import (
	"strings"

	sitter "github.com/odvcencio/gotreesitter"
	"github.com/rfizzle/astimate/internal/lang/duptok"
)

// Func is the complexity of one function: a top-level function
// declaration, a top-level variable initialized with a function or arrow
// function, or a method or function-valued field of a top-level class.
type Func struct {
	// Receiver is the class name of a method or function-valued field,
	// empty for a plain function; Ident the function or member name.
	Receiver, Ident string
	// Cognitive is the cognitive complexity; Nesting the deepest nesting
	// inside the body, which is itself depth 0.
	Cognitive, Nesting int
	// Line is the 1-based line the function starts on.
	Line int
	// Fingerprint hashes the body's normalized tokens (fingerprint.go).
	Fingerprint uint64
}

// Tokens is the normalized token stream of one file, parallel slices: the
// token code, its duplication class, and its first and last 1-based lines.
type Tokens struct {
	Codes      []int32
	Class      []duptok.Class
	Line, Last []int32
}

// Normalized token codes. Every other token kind is interned from
// firstInterned upwards, per module.
const (
	// IdentCode is the code of every identifier.
	IdentCode = int32(0)
	// LiteralCode is the code of every literal.
	LiteralCode   = int32(1)
	firstInterned = int32(2)
)

// Kinds is the token interning table of one module load: token kinds get
// duplication codes in the order they are first seen. It is not safe for
// concurrent use.
type Kinds struct {
	intern map[string]tokenKind
}

// tokenKind is an interned token kind: its duplication code, per module,
// and its fingerprint code, stable across loads.
type tokenKind struct {
	code int32
	fp   uint64
}

// NewKinds returns an empty interning table.
func NewKinds() *Kinds {
	return &Kinds{intern: make(map[string]tokenKind)}
}

// code returns the interned duplication code and the fingerprint code of a
// token kind.
func (k *Kinds) code(kind string) (int32, uint64) {
	t, ok := k.intern[kind]
	if !ok {
		t = tokenKind{code: firstInterned + int32(len(k.intern)), fp: fpKind(kind)}
		k.intern[kind] = t
	}
	return t.code, t.fp
}

// Span is the byte range [Start, End) of one comment.
type Span struct{ Start, End int }

// Walker visits the nodes of one file's tree. What it gathers is left in
// its exported fields, each in source order.
type Walker struct {
	lang  *sitter.Language
	src   []byte
	test  bool
	kinds *Kinds

	// Tokens is the normalized token stream of a non-test file.
	Tokens Tokens
	// Comments holds the byte range of every comment.
	Comments []Span
	// Imports lists every module specifier the file imports, duplicates
	// included; the caller appends the re-export sources it finds.
	Imports []string
	// Idents holds the text of every identifier of a test file; nil for a
	// non-test file.
	Idents map[string]bool
	// Funcs holds the complexity of each function of a non-test file.
	Funcs []Func

	// fn is the function being scored; nil outside functions.
	fn *Func
	// hashing reports that the walk is inside fn's body, whose tokens its
	// fingerprint mixes; self is fn's name as a direct call spells it,
	// empty for a method; selfMember is a method's name as a call through
	// this spells it, empty for a plain function, a constructor, and inside
	// a nested function or class that binds its own this.
	hashing    bool
	self       string
	selfMember string
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

// New returns a walker for the source src of one file parsed with the
// grammar lang, a test file when test is set, interning token kinds in
// kinds, which every file of the module shares.
func New(lang *sitter.Language, src []byte, test bool, kinds *Kinds) *Walker {
	w := &Walker{lang: lang, src: src, test: test, kinds: kinds}
	if test {
		w.Idents = make(map[string]bool)
	}
	return w
}

// Visit walks n and everything below it, outside any nesting.
func (w *Walker) Visit(n *sitter.Node) {
	w.visit(n, state{})
}

// Text returns the source text of n, or "" for nil.
func (w *Walker) Text(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	return n.Text(w.src)
}

// Field returns the child of n in field name, or nil.
func (w *Walker) Field(n *sitter.Node, name string) *sitter.Node {
	return n.ChildByFieldName(name, w.lang)
}

// TrimQuotes returns the contents of a string literal's source text.
func TrimQuotes(s string) string {
	if len(s) >= 2 {
		return s[1 : len(s)-1]
	}
	return s
}

// isIdentType reports whether a named leaf of type typ is an identifier.
func isIdentType(typ string) bool {
	return strings.HasSuffix(typ, "identifier")
}

// Function scores the function fn, the member ident of class receiver or,
// with receiver empty, the plain function ident, walking its children and
// fingerprinting its body. Functions of test files are walked but not
// recorded.
func (w *Walker) Function(receiver, ident string, fn *sitter.Node) {
	score := &Func{Receiver: receiver, Ident: ident, Line: int(fn.StartPoint().Row) + 1, Fingerprint: fpOffset}
	prev, prevHashing, prevSelf, prevMember := w.fn, w.hashing, w.self, w.selfMember
	w.fn, w.self, w.selfMember = score, "", ""
	switch {
	case receiver == "":
		w.self = ident
	case ident != "constructor":
		w.selfMember = ident
	}
	for i := range fn.ChildCount() {
		w.hashing = !w.test && fn.FieldNameForChild(i, w.lang) == "body"
		w.visit(fn.Child(i), state{})
	}
	w.fn, w.hashing, w.self, w.selfMember = prev, prevHashing, prevSelf, prevMember
	if !w.test {
		w.Funcs = append(w.Funcs, *score)
	}
}

// visit walks n and everything below it.
func (w *Walker) visit(n *sitter.Node, st state) {
	cc := n.ChildCount()
	if cc == 0 && n.StartByte() == n.EndByte() {
		// Zero-width tokens: automatic semicolons and missing nodes.
		return
	}
	typ := n.Type(w.lang)
	switch typ {
	case "comment", "html_comment", "hash_bang_line":
		w.Comments = append(w.Comments, Span{int(n.StartByte()), int(n.EndByte())})
		return
	case "import_statement", "call_expression":
		w.importOf(n, typ)
		if w.hashing && typ == "call_expression" && w.isSelfCall(n) {
			w.fn.Fingerprint = fpMix(w.fn.Fingerprint, fpSelfCall)
		}
	}
	if w.fn != nil {
		w.fn.Nesting = max(w.fn.Nesting, st.depth)
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
	if w.selfMember != "" && bindsThis(typ) {
		// A nested function other than an arrow, or a nested class body,
		// binds its own this, so this.m() inside it is not a self-call.
		prevMember := w.selfMember
		w.selfMember = ""
		defer func() { w.selfMember = prevMember }()
	}
	if w.fn != nil && w.complexity(n, typ, st) {
		return
	}
	if typ == "unary_expression" && cc == 2 && isSignedNumber(w, n) {
		w.leaf(n.Child(0), w.Text(n.Child(0)), st, true)
		w.visit(n.Child(1), state{nest: st.nest, depth: st.depth, quiet: st.quiet})
		return
	}
	w.children(n, state{nest: st.nest, depth: st.depth, quiet: st.quiet})
}

// children visits every child of n with st.
func (w *Walker) children(n *sitter.Node, st state) {
	for i := range n.ChildCount() {
		w.visit(n.Child(i), st)
	}
}

// importOf records the specifier of an import declaration, an import =
// require(...) declaration, or a require(...) or import(...) call with a
// string literal argument.
func (w *Walker) importOf(n *sitter.Node, typ string) {
	var spec *sitter.Node
	switch typ {
	case "import_statement":
		spec = w.Field(n, "source")
		// import x = require("y") holds its source one level down.
		for i := 0; spec == nil && i < n.ChildCount(); i++ {
			spec = w.Field(n.Child(i), "source")
		}
	case "call_expression":
		if !w.isModuleLoader(w.Field(n, "function")) {
			return
		}
		args := w.Field(n, "arguments")
		if args == nil || args.NamedChildCount() != 1 {
			return
		}
		if a := args.NamedChild(0); a.Type(w.lang) == "string" {
			spec = a
		}
	}
	if spec != nil {
		w.Imports = append(w.Imports, TrimQuotes(w.Text(spec)))
	}
}

// isModuleLoader reports whether the callee fn is import or require.
func (w *Walker) isModuleLoader(fn *sitter.Node) bool {
	if fn == nil {
		return false
	}
	switch fn.Type(w.lang) {
	case "import":
		return true
	case "identifier":
		return w.Text(fn) == "require"
	}
	return false
}

// leaf records a token: an identifier's text in a test file, and in a
// non-test file the normalized duplication token unless st is quiet, and
// the token's fingerprint code inside a function body unless it is a
// semicolon or the text of a template literal. sign marks a unary + or -
// applied to a numeric literal.
func (w *Walker) leaf(n *sitter.Node, typ string, st state, sign bool) {
	named := n.IsNamed()
	if w.test {
		if named && isIdentType(typ) {
			w.Idents[w.Text(n)] = true
		}
		return
	}
	if st.quiet && (!w.hashing || isTemplateText(typ)) {
		return
	}
	var (
		code  int32
		fp    uint64
		class = duptok.Code
	)
	switch {
	case isLiteral(n, typ):
		code, fp, class = LiteralCode, fpLit, duptok.Literal
	case named && (isIdentType(typ) || typ == "undefined"):
		code, fp = IdentCode, fpIdent
	default:
		key := typ
		if named {
			key = "#" + typ
		}
		code, fp = w.kinds.code(key)
		switch {
		case sign:
			class = duptok.Sign
		case !named && isTablePunct(typ):
			class = duptok.Punct
		}
	}
	if w.hashing && typ != ";" {
		w.fn.Fingerprint = fpMix(w.fn.Fingerprint, fp)
	}
	if st.quiet {
		return
	}
	t := &w.Tokens
	t.Codes = append(t.Codes, code)
	t.Class = append(t.Class, class)
	t.Line = append(t.Line, int32(n.StartPoint().Row)+1)
	t.Last = append(t.Last, int32(n.EndPoint().Row)+1)
}

// isSelfCall reports whether the call expression n calls the function
// being walked: a plain function by its bare name, a method through this.
func (w *Walker) isSelfCall(n *sitter.Node) bool {
	switch {
	case w.self != "":
		callee := w.Field(n, "function")
		return callee != nil && callee.Type(w.lang) == "identifier" && w.Text(callee) == w.self
	case w.selfMember != "":
		return w.isSelfMemberCall(n)
	}
	return false
}

// isSelfMemberCall reports whether the call expression n calls, through
// this, the method being walked: this.m(...) inside method m, including
// the optional forms this?.m(...) and this.m?.(...).
func (w *Walker) isSelfMemberCall(n *sitter.Node) bool {
	callee := w.Field(n, "function")
	if callee == nil || callee.Type(w.lang) != "member_expression" {
		return false
	}
	obj := w.Field(callee, "object")
	return obj != nil && obj.Type(w.lang) == "this" && w.Text(w.Field(callee, "property")) == w.selfMember
}

// bindsThis reports whether a node of type typ gives the code inside it its
// own this: a function or method other than an arrow function, or a class
// body, whose field initializers and static blocks see the inner class.
func bindsThis(typ string) bool {
	switch typ {
	case "function_expression", "function", "generator_function",
		"function_declaration", "generator_function_declaration", "method_definition", "class_body":
		return true
	}
	return false
}

// isTemplateText reports whether a leaf of type typ inside a template
// literal is part of its text rather than of a substitution.
func isTemplateText(typ string) bool {
	return typ == "string_fragment" || typ == "escape_sequence"
}

// isSignedNumber reports whether the two-child unary expression n is a +
// or - applied to a numeric literal.
func isSignedNumber(w *Walker, n *sitter.Node) bool {
	op, arg := n.Child(0), n.Child(1)
	t := op.Type(w.lang)
	return (t == "-" || t == "+") && arg.IsNamed() && arg.Type(w.lang) == "number"
}

// HasChild reports whether n has a direct child of type typ.
func (w *Walker) HasChild(n *sitter.Node, typ string) bool {
	for i := range n.ChildCount() {
		if n.Child(i).Type(w.lang) == typ {
			return true
		}
	}
	return false
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
