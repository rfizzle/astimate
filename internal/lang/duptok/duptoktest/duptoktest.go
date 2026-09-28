// Package duptoktest is test support for measuring variants of the
// duplicate-block rules of package duptok. It records the token stream a
// tokenizer feeds duptok, token by token with its class and lines, finds
// the duplicate blocks of that stream with duptok itself, and returns each
// block as the stream positions of its occurrences, so a measurement can
// rewrite blocks (trim, split, drop) or rewrite the stream (fold tokens)
// and count coverage the way duptok does.
//
// It is not part of the extractor contract and no extractor imports it. It
// exists so the calibration measurements in calibration/notes can be rerun
// without widening duptok's production API: positions are recovered
// through the public Blocks method by giving each token a line number
// equal to its index in its file.
package duptoktest

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/rfizzle/astimate/internal/lang/duptok"
)

// Token is one entry of a recorded stream: a token of a file, or the
// separator duptok appends after each file.
type Token struct {
	// Code is the token's normalized code; a separator's is
	// duptok.SeparatorBase plus the file index.
	Code int32
	// Class is the token's role in the literal-only rule.
	Class duptok.Class
	// File is the index of the file the token lies in, or -1 for a
	// separator.
	File int32
	// Line and Last are the first and last 1-based lines the token spans;
	// both are 0 for a separator.
	Line, Last int32
}

// File is one closed file of a recorded stream.
type File struct {
	// Name is the name given to EndFile.
	Name string
	// Code reports, per 1-based line, whether the line is a source line.
	Code []bool
}

// Recorder records a duptok token stream. It has the Add and EndFile
// methods of duptok.Stream, so a tokenizer can write to it wherever it
// writes to a Stream. Its fields may also be built or rewritten directly,
// for a variant that changes the stream itself; Tokens must then keep the
// layout Add and EndFile produce, each file's tokens followed by its
// separator, with File indexes counting files in that order.
type Recorder struct {
	Tokens []Token
	Files  []File
}

// Add appends one token of the current file, as duptok.Stream.Add does.
func (r *Recorder) Add(code int32, class duptok.Class, line, last int) error {
	if code < 0 || code >= duptok.SeparatorBase {
		return fmt.Errorf("adding code %d: outside [0, %d)", code, duptok.SeparatorBase)
	}
	r.Tokens = append(r.Tokens, Token{
		Code: code, Class: class, File: int32(len(r.Files)),
		Line: int32(line), Last: int32(max(last, line)),
	})
	return nil
}

// EndFile closes the current file, as duptok.Stream.EndFile does.
func (r *Recorder) EndFile(name string, code []bool) {
	fi := int32(len(r.Files))
	r.Files = append(r.Files, File{Name: name, Code: code})
	r.Tokens = append(r.Tokens, Token{Code: duptok.SeparatorBase + fi, File: -1})
}

// Block is one duplicate block given by its occurrences.
type Block struct {
	// Pos holds the index in Recorder.Tokens of the first token of each
	// occurrence, in stream order.
	Pos []int32
	// N is the length of the block in tokens.
	N int32
	// Lead is the element of Pos that duptok.Stream.Blocks lists first,
	// which is the occurrence whose following tokens sort lowest. Codes
	// are the same in every occurrence, but a measurement that reads a
	// parallel stream (identifier text, say) at one occurrence reads it
	// here to match the measurements recorded before this package existed.
	Lead int32
}

// Blocks finds the duplicate blocks of the recorded stream under opts, the
// blocks duptok.Stream.Blocks finds, in the order it returns them. Tokens
// after the last separator are ignored.
func (r *Recorder) Blocks(opts duptok.Options) ([]Block, error) {
	var s duptok.Stream
	// start[f] is the index in Tokens of file f's first token. Each token
	// goes to s with its 1-based index in its file as its line, so an
	// occurrence's StartLine is its offset in the file.
	start := make([]int32, 0, len(r.Files))
	k := 0
	for i, t := range r.Tokens {
		if k == 0 {
			start = append(start, int32(i))
		}
		if t.File < 0 {
			s.EndFile(r.fileName(t.Code-duptok.SeparatorBase), nil)
			k = 0
			continue
		}
		k++
		if err := s.Add(t.Code, t.Class, k, k); err != nil {
			return nil, err
		}
	}
	found, err := s.Blocks(opts)
	if err != nil {
		return nil, err
	}
	out := make([]Block, 0, len(found))
	for _, b := range found {
		pos := make([]int32, len(b.Files))
		for i, f := range b.Files {
			pos[i] = start[f] + int32(b.Locations[i].StartLine) - 1
		}
		lead := pos[0]
		slices.Sort(pos)
		out = append(out, Block{Pos: pos, N: int32(b.Tokens), Lead: lead})
	}
	return out, nil
}

// fileName returns the name of file i, or "" when it is not recorded.
func (r *Recorder) fileName(i int32) string {
	if int(i) < len(r.Files) {
		return r.Files[i].Name
	}
	return ""
}

// Count counts blocks over the recorded stream as duptok.Stream.Count
// does: the number of blocks, covered source lines over sloc as a
// percentage, and every occurrence's location, blocks in order of first
// occurrence. It also returns the covered source lines per file name.
func (r *Recorder) Count(blocks []Block, sloc int) (duptok.Result, map[string]int) {
	res := duptok.Result{Blocks: len(blocks)}
	sorted := slices.Clone(blocks)
	slices.SortStableFunc(sorted, func(a, b Block) int {
		return cmp.Or(cmp.Compare(a.Pos[0], b.Pos[0]), cmp.Compare(a.N, b.N))
	})
	diff := make([]int32, len(r.Tokens)+1)
	for _, b := range sorted {
		for _, p := range b.Pos {
			diff[p]++
			diff[p+b.N]--
			res.Locations = append(res.Locations, r.Location(p, b.N))
		}
	}
	covered := make([][]bool, len(r.Files))
	for i, f := range r.Files {
		covered[i] = make([]bool, len(f.Code))
	}
	depth := int32(0)
	for i, t := range r.Tokens {
		depth += diff[i]
		if depth == 0 || t.File < 0 {
			continue
		}
		cf := covered[t.File]
		for ln := t.Line; ln <= t.Last && int(ln) < len(cf); ln++ {
			cf[ln] = true
		}
	}
	lines, per := 0, make(map[string]int, len(r.Files))
	for i, f := range r.Files {
		for ln, ok := range covered[i] {
			if ok && f.Code[ln] {
				lines++
				per[f.Name]++
			}
		}
	}
	res.Pct = duptok.Percent(lines, sloc)
	return res, per
}

// Location returns the location of the occurrence of n tokens starting at
// stream position p.
func (r *Recorder) Location(p, n int32) duptok.Location {
	return duptok.Location{
		File:      r.Files[r.Tokens[p].File].Name,
		StartLine: int(r.Tokens[p].Line),
		EndLine:   int(r.Tokens[p+n-1].Last),
	}
}
