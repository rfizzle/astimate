package duptok

import "slices"

// Block kinds (SPEC.md 6.3). Every duplicate block is marked by what its
// tokens are made of, from a Shape per token that the language supplies
// (Stream.SetShape): the place the token holds in the syntax around it.
// The first of these that holds names the block's kind:
//
//   - Declarative: no token of any occurrence lies in a statement, and at
//     least one lies in a declaration (ShapeDecl), the rest being
//     ShapeNeutral: table rows, key maps, style structs.
//   - Idiom: every token of every occurrence is ShapeDecl, ShapeNeutral or
//     ShapeIdiom, at least one ShapeIdiom: a run of error returns, call
//     statements and assignments of call results with no other control
//     flow.
//   - SelfOverlap: an occurrence starts on or before the last line of the
//     previous occurrence of the same block in the same file, as a run of
//     like calls matching itself a line or two down does.
//   - Code: anything else, a block made only of ShapeNeutral tokens and
//     every block of a stream whose language supplies no shapes included.
//
// Code blocks are counted apart (Result.Code, dup_blocks_code), so a gate
// can judge copied logic without the boilerplate around it.

// Shape is what the syntax around a token makes of it, for block kinds.
// The zero value is ShapeCode, so a token without a shape counts as code.
type Shape uint8

const (
	// ShapeCode is a token of a statement that is not an idiom: control
	// flow, a plain return, an increment, an expression of operators.
	ShapeCode Shape = iota
	// ShapeNeutral is a token outside every statement and declaration
	// body: a package or import clause, a function header, the braces of
	// a function body.
	ShapeNeutral
	// ShapeDecl is a token inside a composite literal, a struct type
	// declaration or a constant or variable declaration.
	ShapeDecl
	// ShapeIdiom is a token of an idiomatic statement: an error return, a
	// bare call statement or an assignment of a call result.
	ShapeIdiom
)

// Kind is what a duplicate block is made of; see the comment above.
type Kind uint8

const (
	// KindCode is copied logic, the kind dup_blocks_code counts.
	KindCode Kind = iota
	// KindDeclarative is a block of declarations and literals only.
	KindDeclarative
	// KindIdiom is a block of idiomatic statements.
	KindIdiom
	// KindSelfOverlap is a block whose occurrences overlap one another.
	KindSelfOverlap
)

// String returns the kind as metrics.DupBlock and metrics.CrossBlock name
// it: "code", "declarative", "idiom" or "self-overlap".
func (k Kind) String() string {
	switch k {
	case KindDeclarative:
		return "declarative"
	case KindIdiom:
		return "idiom"
	case KindSelfOverlap:
		return "self-overlap"
	}
	return "code"
}

// SetShape sets the shape of the token added last. It does nothing on an
// empty stream. A stream none of whose tokens had a shape set marks every
// block code or self-overlap.
func (s *Stream) SetShape(shape Shape) {
	n := len(s.codes)
	if n == 0 {
		return
	}
	if s.shape == nil {
		s.shape = make([]Shape, n, cap(s.codes))
	}
	s.shape[n-1] = shape
}

// shapeAt returns the shape of the token at stream position p, ShapeCode
// when none was set.
func (s *Stream) shapeAt(p int32) Shape {
	if int(p) < len(s.shape) {
		return s.shape[p]
	}
	return ShapeCode
}

// kind returns the kind of the block of n tokens whose occurrences start
// at the stream positions pos, which it sorts.
func (s *Stream) kind(pos []int32, n int32) Kind {
	var seen [ShapeIdiom + 1]bool
	for _, p := range pos {
		for i := p; i < p+n; i++ {
			seen[s.shapeAt(i)] = true
		}
	}
	switch {
	case seen[ShapeCode]:
	case seen[ShapeIdiom]:
		return KindIdiom
	case seen[ShapeDecl]:
		return KindDeclarative
	}
	slices.Sort(pos)
	for i := 1; i < len(pos); i++ {
		prev, cur := pos[i-1], pos[i]
		if s.file[prev] == s.file[cur] && s.line[cur] <= s.last[prev+n-1] {
			return KindSelfOverlap
		}
	}
	return KindCode
}
