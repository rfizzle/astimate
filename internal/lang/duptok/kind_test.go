package duptok

import (
	"slices"
	"testing"
)

// shapedFile adds one file to s: the codes base..base+n-1, each of class
// Code on its own line, with the shapes of shapes in turn (none set when
// shapes is nil), then closes it under name.
func shapedFile(t *testing.T, s *Stream, name string, codes []int32, shapes []Shape) {
	t.Helper()
	for i, c := range codes {
		if err := s.Add(c, Code, i+1, i+1); err != nil {
			t.Fatal(err)
		}
		if shapes != nil {
			s.SetShape(shapes[i])
		}
	}
	lines := make([]bool, len(codes)+2)
	for i := range lines {
		lines[i] = i > 0
	}
	s.EndFile(name, lines)
}

// shapeRun returns n copies of sh.
func shapeRun(n int, sh Shape) []Shape {
	return slices.Repeat([]Shape{sh}, n)
}

// codeRun returns the codes from..from+n-1.
func codeRun(from int32, n int) []int32 {
	out := make([]int32, n)
	for i := range out {
		out[i] = from + int32(i)
	}
	return out
}

func TestKinds(t *testing.T) {
	const n = 10
	body := codeRun(1, n)
	mixed := slices.Concat(shapeRun(4, ShapeNeutral), shapeRun(6, ShapeDecl))
	cases := []struct {
		name   string
		shapes [2][]Shape
		want   Kind
	}{
		{"no shapes set is code", [2][]Shape{nil, nil}, KindCode},
		{"declarations is declarative", [2][]Shape{shapeRun(n, ShapeDecl), shapeRun(n, ShapeDecl)}, KindDeclarative},
		{"declarations and neutral is declarative", [2][]Shape{mixed, mixed}, KindDeclarative},
		{"neutral only is code", [2][]Shape{shapeRun(n, ShapeNeutral), shapeRun(n, ShapeNeutral)}, KindCode},
		{"idiom with declarations is idiom", [2][]Shape{slices.Concat(shapeRun(5, ShapeIdiom), shapeRun(5, ShapeDecl)), shapeRun(n, ShapeIdiom)}, KindIdiom},
		{"one code token in one occurrence is code", [2][]Shape{shapeRun(n, ShapeIdiom), slices.Concat(shapeRun(9, ShapeIdiom), []Shape{ShapeCode})}, KindCode},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var s Stream
			shapedFile(t, &s, "a.go", slices.Concat([]int32{100}, body, []int32{101}), withEnds(tc.shapes[0]))
			shapedFile(t, &s, "b.go", slices.Concat([]int32{102}, body, []int32{103}), withEnds(tc.shapes[1]))
			res, err := s.Count(Options{MinTokens: 5}, 20)
			if err != nil {
				t.Fatal(err)
			}
			if res.Blocks != 1 {
				t.Fatalf("Blocks = %d, want 1", res.Blocks)
			}
			if got := res.Groups()[0].Kind; got != tc.want {
				t.Errorf("kind = %v, want %v", got, tc.want)
			}
			wantCode := 0
			if tc.want == KindCode {
				wantCode = 1
			}
			if res.Code != wantCode {
				t.Errorf("Code = %d, want %d", res.Code, wantCode)
			}
			blocks, err := s.Blocks(Options{MinTokens: 5})
			if err != nil {
				t.Fatal(err)
			}
			if len(blocks) != 1 || blocks[0].Kind != tc.want {
				t.Errorf("Blocks = %+v, want one block of kind %v", blocks, tc.want)
			}
		})
	}
}

// withEnds returns shapes with a code shape before and after it, for the
// distinct tokens around a block; nil stays nil.
func withEnds(shapes []Shape) []Shape {
	if shapes == nil {
		return nil
	}
	return slices.Concat([]Shape{ShapeCode}, shapes, []Shape{ShapeCode})
}

func TestKindSelfOverlap(t *testing.T) {
	// A tandem run a b a b ... matches itself shifted by one period: the
	// occurrences overlap, so the block is self-overlap, unless its tokens
	// make it an idiom first.
	run := slices.Concat([]int32{9}, slices.Repeat([]int32{1, 2}, 8), []int32{8})
	for _, tc := range []struct {
		name  string
		shape Shape
		want  Kind
	}{
		{"code", ShapeCode, KindSelfOverlap},
		{"idiom", ShapeIdiom, KindIdiom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var s Stream
			shapedFile(t, &s, "a.go", run, withEnds(shapeRun(len(run)-2, tc.shape)))
			res, err := s.Count(Options{MinTokens: 4}, 20)
			if err != nil {
				t.Fatal(err)
			}
			if res.Blocks != 1 || res.Groups()[0].Kind != tc.want {
				t.Fatalf("blocks %d, kinds %v, want one of kind %v", res.Blocks, res.Kinds, tc.want)
			}
		})
	}
}

func TestKindString(t *testing.T) {
	for k, want := range map[Kind]string{KindCode: "code", KindDeclarative: "declarative", KindIdiom: "idiom", KindSelfOverlap: "self-overlap"} {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

func TestDetailsCarryKind(t *testing.T) {
	_, blocks := Details([]Group{{Kind: KindIdiom, Locations: []Location{{"a.go", 1, 2}, {"b.go", 3, 4}}}}, nil)
	if len(blocks) != 1 || blocks[0].Kind != "idiom" {
		t.Fatalf("Details blocks = %+v, want one of kind idiom", blocks)
	}
}
