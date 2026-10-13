package walk

import (
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/duptok"
)

// tsRows are five object literal rows, 70 tokens; tsRowsRenamed are the
// same rows with other values.
const (
	tsRows        = "  { x: 1, y: 2, n: \"a\" },\n  { x: 3, y: 4, n: \"b\" },\n  { x: 5, y: 6, n: \"c\" },\n  { x: 7, y: 8, n: \"d\" },\n  { x: 9, y: 10, n: \"e\" },\n"
	tsRowsRenamed = "  { x: 11, y: 12, n: \"f\" },\n  { x: 13, y: 14, n: \"g\" },\n  { x: 15, y: 16, n: \"h\" },\n  { x: 17, y: 18, n: \"i\" },\n  { x: 19, y: 20, n: \"j\" },\n"
)

// tsChain is a function of three awaited call declarations, each followed
// by a guard clause, before tail.
func tsChain(name, tail string) string {
	return "async function " + name + "(s: string) {\n" +
		"  const a = await load(s);\n  if (!a) {\n    return null;\n  }\n" +
		"  const b = await parse(a);\n  if (!b) {\n    return null;\n  }\n" +
		"  const c = await check(b);\n  if (!c) {\n    return null;\n  }\n" +
		tail + "\n}\n"
}

// dupOf walks src as one non-test file and counts its duplicate blocks.
func dupOf(t *testing.T, src string) duptok.Result {
	t.Helper()
	w := walkFile(t, src, false, NewKinds())
	tk := &w.Tokens
	if len(tk.Shape) != len(tk.Codes) {
		t.Fatalf("%d shapes for %d tokens", len(tk.Shape), len(tk.Codes))
	}
	var s duptok.Stream
	for i, c := range tk.Codes {
		if err := s.AddKeyed(c, tk.Class[i], int(tk.Line[i]), int(tk.Last[i]), tk.Keys[i]); err != nil {
			t.Fatal(err)
		}
		s.SetShape(tk.Shape[i])
	}
	lines := make([]bool, strings.Count(src, "\n")+2)
	s.EndFile("a.ts", lines)
	res, err := s.Count(duptok.DefaultOptions(), 100)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestBlockKinds(t *testing.T) {
	cases := []struct {
		name string
		src  string
		kind duptok.Kind
	}{
		{
			name: "table of object literals is declarative",
			src:  "type P = { x: number; y: number; n: string };\n\nconst A: P[] = [\n" + tsRows + "];\n\nconst B: P[] = [\n" + tsRowsRenamed + "];\n",
			kind: duptok.KindDeclarative,
		},
		{
			name: "the same rows returned from function bodies are code",
			src:  "function a(): P[] {\n  return [\n" + tsRows + "  ];\n}\n\nfunction b(): P[] {\n  return [\n" + tsRowsRenamed + "  ];\n}\n",
			kind: duptok.KindCode,
		},
		{
			name: "three guard chains are idiom",
			src:  tsChain("a", "  return build(c);") + "\n" + tsChain("b", "  use(c);\n  return null;"),
			kind: duptok.KindIdiom,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := dupOf(t, tc.src)
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

func TestShapes(t *testing.T) {
	// One token of each statement the rules name, by the text of its
	// first token.
	src := "const t = { a: 1 };\nlet r = f();\nlet s = a + b;\ng(1);\nfor (;;) {}\nfunction h() {\n  if (e) throw e;\n  x = y;\n}\n"
	w := walkFile(t, src, false, NewKinds())
	got := make(map[int]duptok.Shape)
	for i, ln := range w.Tokens.Line {
		if _, ok := got[int(ln)]; !ok {
			got[int(ln)] = w.Tokens.Shape[i]
		}
	}
	want := map[int]duptok.Shape{
		1: duptok.ShapeDecl, 2: duptok.ShapeIdiom, 3: duptok.ShapeCode, 4: duptok.ShapeIdiom,
		5: duptok.ShapeCode, 6: duptok.ShapeNeutral, 7: duptok.ShapeIdiom, 8: duptok.ShapeCode,
	}
	for ln, sh := range want {
		if got[ln] != sh {
			t.Errorf("line %d: first token shape %d, want %d", ln, got[ln], sh)
		}
	}
}
