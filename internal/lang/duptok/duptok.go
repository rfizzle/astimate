// Package duptok finds duplicate blocks in a normalized token stream
// (SPEC.md section 6.3), independently of the language the tokens came
// from. An extractor scans each non-test file of a package into a Stream,
// one int32 code and one Class per token, and Count returns dup_blocks,
// duplication_pct and the location of every occurrence.
//
// The finder is the one internal/lang/golang uses, adapted to abstract
// token classes: a suffix array over the stream built by prefix doubling
// with counting sorts, the LCP array by Kasai's algorithm, and a single
// sweep that keeps each maximal repeat of at least MinTokens codes that is
// not wholly inside an occurrence of a longer repeat. See the file comment
// of internal/lang/golang/duplication.go for the derivation. Each file is
// followed by a separator code unique in the stream, so no repeat crosses a
// file boundary.
//
// Literal-only blocks. With Options.IgnoreLiteralOnly, a surviving block
// whose every token is of class Literal or Punct is dropped before
// counting; with Options.FoldSigns a token of class Sign counts as part of
// the literal after it. The language decides which tokens belong to which
// class; the rule itself is language-agnostic.
//
// Coverage. A line is covered when a token of any occurrence of any block
// starts or ends on it, or lies between, and counts only when the language
// marked it as a source line. duplication_pct is covered lines over the
// package SLOC, times 100, rounded to one decimal.
package duptok

import (
	"cmp"
	"fmt"
	"math"
	"slices"
)

// SeparatorBase is the code of the separator after the first file; the
// separator after file i is SeparatorBase + i. Token codes must be below
// it.
const SeparatorBase = int32(1 << 30)

// Class is the role a token plays in the literal-only rule.
type Class uint8

const (
	// Code is any token that makes a block code rather than data: an
	// identifier, a keyword or an operator.
	Code Class = iota
	// Literal is a string, numeric or other literal.
	Literal
	// Punct is one of the punctuation tokens a literal table is written
	// with: , { } : [ ] ( ) and an explicit ;.
	Punct
	// Sign is a unary + or - directly before a numeric literal. It counts
	// as part of the literal when Options.FoldSigns is on, and as Code
	// otherwise.
	Sign
)

// Options configures duplicate detection.
type Options struct {
	// MinTokens is duplication.min_tokens: the shortest normalized token
	// sequence that counts as a duplicate block. It must be at least 1.
	MinTokens int
	// IgnoreLiteralOnly is duplication.ignore_literal_only: drop a block
	// made only of Literal and Punct tokens.
	IgnoreLiteralOnly bool
	// FoldSigns is duplication.fold_signs: under IgnoreLiteralOnly, a Sign
	// token counts as a literal.
	FoldSigns bool
}

// DefaultOptions returns the SPEC.md defaults: 40 tokens, literal-only
// blocks ignored, signed literals counted as literals.
func DefaultOptions() Options {
	return Options{MinTokens: 40, IgnoreLiteralOnly: true, FoldSigns: true}
}

// Location is one occurrence of a duplicate block: the file name given to
// EndFile and the first and last 1-based lines holding its tokens.
type Location struct {
	File               string
	StartLine, EndLine int
}

// Result holds the duplication metrics of one package.
type Result struct {
	// Blocks is dup_blocks: distinct maximal repeated sequences after
	// merging.
	Blocks int
	// Pct is duplication_pct: covered SLOC over package SLOC times 100,
	// rounded to one decimal.
	Pct float64
	// Locations lists every occurrence of every block, block by block in
	// order of first occurrence, occurrences in stream order.
	Locations []Location
}

// file is one scanned file of the stream.
type file struct {
	name string
	// code reports, per 1-based line, whether the line is a source line.
	code []bool
}

// Stream is the normalized token stream of a package. The zero value is
// empty and ready to use. It is not safe for concurrent use.
type Stream struct {
	codes      []int32
	class      []Class
	file       []int32
	line, last []int32
	files      []file
}

// Add appends one token of the current file: its code, its class, and the
// first and last 1-based lines it spans. Tokens added after the last
// EndFile belong to the next file. A code outside [0, SeparatorBase) is an
// error.
func (s *Stream) Add(code int32, class Class, line, last int) error {
	if code < 0 || code >= SeparatorBase {
		return fmt.Errorf("adding code %d: outside [0, %d)", code, SeparatorBase)
	}
	s.codes = append(s.codes, code)
	s.class = append(s.class, class)
	s.file = append(s.file, int32(len(s.files)))
	s.line = append(s.line, int32(line))
	s.last = append(s.last, int32(max(last, line)))
	return nil
}

// EndFile closes the current file under name, with code marking its source
// lines by 1-based index (index 0 is unused), and appends its separator.
func (s *Stream) EndFile(name string, code []bool) {
	fi := int32(len(s.files))
	s.files = append(s.files, file{name: name, code: code})
	s.codes = append(s.codes, SeparatorBase+fi)
	s.class = append(s.class, Code)
	s.file = append(s.file, -1)
	s.line = append(s.line, 0)
	s.last = append(s.last, 0)
}

// Count finds the duplicate blocks of the stream under opts and returns
// their count, their coverage as a percentage of sloc, and their
// locations. Tokens added after the last EndFile are ignored.
func (s *Stream) Count(opts Options, sloc int) (Result, error) {
	if opts.MinTokens < 1 {
		return Result{}, fmt.Errorf("finding duplicates: minimum of %d tokens is not positive", opts.MinTokens)
	}
	s.trim()
	sa, reps := find(s.codes, opts.MinTokens)
	if opts.IgnoreLiteralOnly {
		reps = s.dropLiteralOnly(sa, reps, opts.FoldSigns)
	}
	return s.count(sa, reps, sloc), nil
}

// trim drops tokens added after the last separator, so the stream ends
// with a unique code as find requires.
func (s *Stream) trim() {
	n := len(s.codes)
	for n > 0 && s.file[n-1] >= 0 {
		n--
	}
	s.codes, s.class, s.file = s.codes[:n], s.class[:n], s.file[:n]
	s.line, s.last = s.line[:n], s.last[:n]
}

// repeat is one maximal repeat: its occurrences are sa[lb..rb] and its
// length is n tokens.
type repeat struct {
	lb, rb, n int32
}

// dropLiteralOnly returns reps without the repeats whose tokens are all
// literals or literal-table punctuation, reusing the backing array. With
// signs, a Sign token counts as a literal.
func (s *Stream) dropLiteralOnly(sa []int32, reps []repeat, signs bool) []repeat {
	return slices.DeleteFunc(reps, func(r repeat) bool {
		p := sa[r.lb]
		for _, c := range s.class[p : p+r.n] {
			switch c {
			case Literal, Punct:
			case Sign:
				if !signs {
					return false
				}
			default:
				return false
			}
		}
		return true
	})
}

// count turns the repeats found in the stream, as intervals of its suffix
// array sa, into block count, coverage percentage over sloc, and occurrence
// locations.
func (s *Stream) count(sa []int32, reps []repeat, sloc int) Result {
	if len(reps) == 0 {
		return Result{}
	}
	// Blocks in order of first occurrence, occurrences in stream order.
	type block struct {
		n   int32
		pos []int32
	}
	blocks := make([]block, 0, len(reps))
	for _, r := range reps {
		pos := slices.Clone(sa[r.lb : r.rb+1])
		slices.Sort(pos)
		blocks = append(blocks, block{n: r.n, pos: pos})
	}
	slices.SortFunc(blocks, func(a, b block) int {
		return cmp.Or(cmp.Compare(a.pos[0], b.pos[0]), cmp.Compare(a.n, b.n))
	})

	diff := make([]int32, len(s.codes)+1)
	res := Result{Blocks: len(blocks)}
	for _, b := range blocks {
		for _, p := range b.pos {
			diff[p]++
			diff[p+b.n]--
			end := p + b.n - 1
			res.Locations = append(res.Locations, Location{
				File:      s.files[s.file[p]].name,
				StartLine: int(s.line[p]),
				EndLine:   int(s.last[end]),
			})
		}
	}

	covered := make([][]bool, len(s.files))
	for i, f := range s.files {
		covered[i] = make([]bool, len(f.code))
	}
	depth := int32(0)
	for t := range s.codes {
		depth += diff[t]
		if depth == 0 || s.file[t] < 0 {
			continue
		}
		cf := covered[s.file[t]]
		for ln := s.line[t]; ln <= s.last[t] && int(ln) < len(cf); ln++ {
			cf[ln] = true
		}
	}
	lines := 0
	for i, f := range s.files {
		for ln, ok := range covered[i] {
			if ok && f.code[ln] {
				lines++
			}
		}
	}
	res.Pct = Percent(lines, sloc)
	return res
}

// Percent returns covered / sloc * 100 rounded to one decimal, or 0 when
// sloc is 0.
func Percent(covered, sloc int) float64 {
	if sloc <= 0 {
		return 0
	}
	return math.Round(float64(covered)/float64(sloc)*1000) / 10
}
