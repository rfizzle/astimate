package walk

import (
	"slices"
	"testing"

	sitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// parse parses src with the TypeScript grammar and returns its root node,
// releasing the tree when the test ends.
func parse(t *testing.T, src string) *sitter.Node {
	t.Helper()
	tree, err := sitter.NewParser(grammars.TypescriptLanguage()).ParseStrict([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Release)
	return tree.RootNode()
}

// walkFile visits every top-level statement of src, scoring each function
// declaration as a plain function as the inspect package does, and returns
// the walker.
func walkFile(t *testing.T, src string, test bool, kinds *Kinds) *Walker {
	t.Helper()
	lang := grammars.TypescriptLanguage()
	root := parse(t, src)
	w := New(lang, []byte(src), test, kinds)
	for i := range root.ChildCount() {
		n := root.Child(i)
		if n.Type(lang) == "function_declaration" {
			w.Function("", w.Text(w.Field(n, "name")), n)
			continue
		}
		w.Visit(n)
	}
	return w
}

func TestWalker(t *testing.T) {
	const src = "// c\nimport { a } from \"./a\";\nfunction f(n: number) {\n  if (n > 0 && n < 9) { return f(n - 1); }\n  return require(\"b\");\n}\nx(1);\n"
	w := walkFile(t, src, false, NewKinds())
	if want := []string{"./a", "b"}; !slices.Equal(w.Imports, want) {
		t.Errorf("Imports = %q, want %q", w.Imports, want)
	}
	if len(w.Comments) != 1 || w.Comments[0] != (Span{Start: 0, End: 4}) {
		t.Errorf("Comments = %+v, want the one comment at [0, 4)", w.Comments)
	}
	if len(w.Funcs) != 1 {
		t.Fatalf("Funcs = %+v, want one", w.Funcs)
	}
	f := w.Funcs[0]
	if f.Ident != "f" || f.Receiver != "" || f.Line != 3 || f.Cognitive != 2 || f.Nesting != 1 || f.Fingerprint == fpOffset {
		t.Errorf("Func = %+v, want f on line 3, cognitive 2, nesting 1, fingerprinted", f)
	}
	n := len(w.Tokens.Codes)
	if n == 0 || len(w.Tokens.Class) != n || len(w.Tokens.Line) != n || len(w.Tokens.Last) != n {
		t.Errorf("Tokens = %+v, want parallel non-empty slices", w.Tokens)
	}
	if w.Idents != nil {
		t.Errorf("Idents of a non-test file = %v, want nil", w.Idents)
	}
}

func TestWalkerTestFile(t *testing.T) {
	w := walkFile(t, "function helper() { return a.b; }\nit(\"works\", () => helper());\n", true, NewKinds())
	for _, id := range []string{"helper", "a", "b", "it"} {
		if !w.Idents[id] {
			t.Errorf("identifier %q not recorded in %v", id, w.Idents)
		}
	}
	if len(w.Funcs) != 0 || len(w.Tokens.Codes) != 0 {
		t.Errorf("test file recorded functions %v or %d tokens", w.Funcs, len(w.Tokens.Codes))
	}
}

func TestKindsShared(t *testing.T) {
	// Codes are interned per module: the same kinds get the same codes in
	// every file walked with one table.
	kinds := NewKinds()
	a := walkFile(t, "x(1);\n", false, kinds)
	b := walkFile(t, "y(2);\n", false, kinds)
	if !slices.Equal(a.Tokens.Codes, b.Tokens.Codes) {
		t.Errorf("codes = %v and %v, want equal", a.Tokens.Codes, b.Tokens.Codes)
	}
	if a.Tokens.Codes[0] != IdentCode || a.Tokens.Codes[2] != LiteralCode {
		t.Errorf("codes = %v, want an identifier then ( then a literal", a.Tokens.Codes)
	}
}

func TestHasChild(t *testing.T) {
	const src = "outer: for (;;) { break outer; }\n"
	lang := grammars.TypescriptLanguage()
	root := parse(t, src)
	w := New(lang, []byte(src), false, NewKinds())
	stmt := root.Child(0)
	if !w.HasChild(stmt, "statement_identifier") {
		t.Errorf("HasChild(%s, statement_identifier) = false, want true", stmt.Type(lang))
	}
	if w.HasChild(stmt, "if_statement") {
		t.Error("HasChild(labeled statement, if_statement) = true, want false")
	}
	if got := w.Text(nil); got != "" {
		t.Errorf("Text(nil) = %q, want empty", got)
	}
	if got := w.Text(w.Field(stmt, "label")); got != "outer" {
		t.Errorf("Text(Field(label)) = %q, want outer", got)
	}
}

func TestTrimQuotes(t *testing.T) {
	for in, want := range map[string]string{`"a"`: "a", "'b'": "b", "`c`": "c", `""`: "", "x": "x"} {
		if got := TrimQuotes(in); got != want {
			t.Errorf("TrimQuotes(%q) = %q, want %q", in, got, want)
		}
	}
}
