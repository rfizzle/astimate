package dup

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/duptok"
)

// kindRows are five struct literal rows, 70 tokens, the table the kind
// tests repeat; kindRowsRenamed are the same rows with other values.
const (
	kindRows = `	{X: 1, Y: 2, N: "a"},
	{X: 3, Y: 4, N: "b"},
	{X: 5, Y: 6, N: "c"},
	{X: 7, Y: 8, N: "d"},
	{X: 9, Y: 10, N: "e"},
`
	kindRowsRenamed = `	{X: 11, Y: 12, N: "f"},
	{X: 13, Y: 14, N: "g"},
	{X: 15, Y: 16, N: "h"},
	{X: 17, Y: 18, N: "i"},
	{X: 19, Y: 20, N: "j"},
`
)

// kindChain is a function of three call assignments, each followed by an
// error return, before tail.
func kindChain(name, tail string) string {
	return "func " + name + `(s string) (*T, error) {
	a, err := load(s)
	if err != nil {
		return nil, err
	}
	b, err := parse(a)
	if err != nil {
		return nil, err
	}
	c, err := check(b)
	if err != nil {
		return nil, err
	}
` + tail + "\n}\n"
}

// kindCalls is a function body of five registration calls before tail.
func kindCalls(tail string) string {
	return "\troot.Add(serve(cfg), 1)\n\troot.Flags().Set(\"a\", \"b\")\n\tlog.Printf(\"%d\", count(x, y))\n" +
		"\troot.Use(mw.Logger())\n\tcfg.Bind(root.PersistentFlags(), \"x\")\n\t" + tail + "\n}\n"
}

// TestBlockKinds checks the kind of the one block of each source set and
// dup_blocks_code beside dup_blocks.
func TestBlockKinds(t *testing.T) {
	cases := []struct {
		name string
		srcs []string
		kind duptok.Kind
	}{
		{
			name: "table of struct literals is declarative",
			srcs: []string{"package a\n\ntype P struct {\n\tX, Y int\n\tN  string\n}\n\nvar A = []P{\n" + kindRows + "}\n\nvar B = []P{\n" + kindRowsRenamed + "}\n"},
			kind: duptok.KindDeclarative,
		},
		{
			name: "the same rows returned from function bodies are code",
			srcs: []string{"package a\n\nfunc A() []P {\n\treturn []P{\n" + kindRows + "\t}\n}\n\nfunc B() []P {\n\treturn []P{\n" + kindRowsRenamed + "\t}\n}\n"},
			kind: duptok.KindCode,
		},
		{
			name: "three error-return chains are idiom",
			srcs: []string{"package a\n\n" + kindChain("A", "\treturn build(c), nil") + "\n" + kindChain("B", "\tuse(c)\n\treturn nil, nil")},
			kind: duptok.KindIdiom,
		},
		{
			name: "a run of like statements matching itself is self-overlap",
			srcs: []string{"package a\n\nfunc A(w func(int) int) (n int) {\n" + strings.Repeat("\tn += w(1) * 2\n", 10) + "\treturn n\n}\n"},
			kind: duptok.KindSelfOverlap,
		},
		{
			name: "registration calls in main.go are declarative",
			srcs: []string{
				"package main\n\nfunc main() {\n" + kindCalls("root.Run()"),
				"package main\n\nfunc main() {\n" + kindCalls("root.Execute(1)"),
			},
			kind: duptok.KindDeclarative,
		},
		{
			name: "the same calls outside main.go are idiom",
			srcs: []string{"package a\n\nfunc A() {\n" + kindCalls("root.Run()") + "\nfunc B() {\n" + kindCalls("return")},
			kind: duptok.KindIdiom,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := dupOfSources(t, DefaultOptions(), tc.srcs...)
			if res.Blocks != 1 {
				t.Fatalf("dup_blocks = %d at %v, want 1", res.Blocks, res.Locations)
			}
			if g := res.Groups()[0].Kind; g != tc.kind {
				t.Errorf("kind = %v at %v, want %v", g, res.Locations, tc.kind)
			}
			want := 0
			if tc.kind == duptok.KindCode {
				want = 1
			}
			if res.Code != want {
				t.Errorf("dup_blocks_code = %d, want %d", res.Code, want)
			}
		})
	}
}

// TestTableBlockLength pins that the repeated table of TestBlockKinds is a
// block of at least 60 tokens.
func TestTableBlockLength(t *testing.T) {
	res := dupOfSources(t, DefaultOptions(), "package a\n\nvar A = []P{\n"+kindRows+"}\n\nvar B = []P{\n"+kindRowsRenamed+"}\n")
	if res.Blocks != 1 || res.Tokens[0] < 60 {
		t.Fatalf("%d blocks of %v tokens, want one of at least 60", res.Blocks, res.Tokens)
	}
}

// TestShapes checks the shape of the first token of each line of a source
// holding one statement or declaration of each rule.
func TestShapes(t *testing.T) {
	const src = "package a\n" +
		"var t = T{A: 1}\n" +
		"func f() {\n" +
		"\tr := g()\n" +
		"\ts := a + b\n" +
		"\th(1)\n" +
		"\tfor {\n" +
		"\t\tx++\n" +
		"\t}\n" +
		"\tif err != nil {\n" +
		"\t\treturn\n" +
		"\t}\n" +
		"}\n"
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "a.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	tf := fs.File(f.FileStart)
	sh := &shapes{spans: shapeSpans(tf, f)}
	want := []duptok.Shape{
		duptok.ShapeNeutral, duptok.ShapeDecl, duptok.ShapeNeutral, duptok.ShapeIdiom, duptok.ShapeCode,
		duptok.ShapeIdiom, duptok.ShapeCode, duptok.ShapeCode, duptok.ShapeCode, duptok.ShapeIdiom,
		duptok.ShapeIdiom, duptok.ShapeIdiom, duptok.ShapeNeutral,
	}
	for i, line := range strings.SplitAfter(strings.TrimSuffix(src, "\n"), "\n") {
		off := tf.Offset(tf.LineStart(i+1)) + len(line) - len(strings.TrimLeft(line, "\t"))
		if got := sh.at(off); got != want[i] {
			t.Errorf("line %d %q: shape %d, want %d", i+1, strings.TrimSpace(line), got, want[i])
		}
	}
	if v := (spanWalker{}).Visit(nil); v != nil {
		t.Errorf("Visit(nil) = %v, want nil", v)
	}
}
