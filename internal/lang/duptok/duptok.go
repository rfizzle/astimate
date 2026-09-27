// Package duptok finds duplicate blocks in a normalized token stream
// (SPEC.md section 6.3), independently of the language the tokens came
// from. An extractor scans each non-test file of a package into a Stream,
// one int32 code and one Class per token, and Count returns dup_blocks,
// duplication_pct and the location of every occurrence; Blocks returns the
// files and locations of each block's occurrences, for passes such as the
// Go extractor's cross-package count that attribute blocks rather than
// measure coverage.
//
// Stream. Each file is followed by a separator code, SeparatorBase plus the
// file's index. Separators are unique in the stream and above every token
// code, so no common prefix of two suffixes, and so no repeat, can run
// across a file boundary. Because every token is exactly one code, a match
// can never start or end inside a token.
//
// Finder. A suffix array over the int32 stream is built by prefix doubling
// with counting sorts (O(n log n)), then the LCP array by Kasai's
// algorithm. index/suffixarray was considered and not used: it indexes
// bytes and only answers lookups, it does not expose the sorted suffixes or
// their common prefix lengths that maximal-repeat enumeration needs. A
// rolling hash would need verification and a separate maximality pass; the
// suffix array gives both exactly.
//
// A maximal repeat is a sequence occurring at least twice that cannot be
// extended left or right with all occurrences agreeing; in the suffix array
// it is an LCP interval whose occurrences are not all preceded by the same
// code. SPEC.md merging drops a maximal repeat each of whose occurrences
// lies inside an occurrence of a longer repeat (nested repeats, and the
// shorter periods of a tandem run); the survivors are the duplicate blocks.
//
// Enumerating every maximal repeat and testing containment occurrence by
// occurrence is quadratic on long periodic runs such as literal tables, so
// the survivors are found directly. Let L(q) be the longest repeat starting
// at stream position q (the larger LCP with q's two suffix-array
// neighbours). An occurrence [q, q+l) lies inside a longer repeat exactly
// when l < L(q) or some q' < q has q'+L(q') >= q+l, since any repeated
// segment extends to an occurrence of a maximal repeat at least as long.
// So, sweeping q upwards with reach = max(q'+L(q')) over q' < q, a position
// with L(q) >= MinTokens and q+L(q) > reach starts an occurrence of a
// surviving block: the sequence of length L(q) at q, which is then
// left-maximal too (otherwise q-1 would reach as far). Its LCP interval,
// found from previous and next smaller LCP values, gives all its
// occurrences; blocks are the distinct intervals. Everything after the
// suffix array is linear. The occurrences of the blocks, overlapping or
// not, are unioned for coverage; every shorter repeat lies inside them, so
// the union is the same as over all repeats.
//
// Literal-only blocks. With Options.IgnoreLiteralOnly, a surviving block
// whose every token is of class Literal or Punct is dropped before
// counting; with Options.FoldSigns a token of class Sign counts as part of
// the literal after it. The rule runs after merging, so a block that holds
// a literal table next to code is kept whole. The language decides which
// tokens belong to which class; the rule itself is language-agnostic.
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

// Files returns the number of files closed by EndFile.
func (s *Stream) Files() int {
	return len(s.files)
}

// Count finds the duplicate blocks of the stream under opts and returns
// their count, their coverage as a percentage of sloc, and their
// locations. Tokens added after the last EndFile are ignored.
func (s *Stream) Count(opts Options, sloc int) (Result, error) {
	sa, reps, err := s.find(opts)
	if err != nil {
		return Result{}, err
	}
	return s.count(sa, reps, sloc), nil
}

// Block is one duplicate block as Blocks reports it.
type Block struct {
	// Tokens is the length of the block in tokens.
	Tokens int
	// Files holds, for each occurrence of the block, the index of the file
	// it lies in, counting files from 0 in EndFile order. Occurrences are
	// in no particular order, and a file holding several occurrences
	// appears once per occurrence.
	Files []int32
	// Locations holds the location of each occurrence, in the same order
	// as Files: Locations[i] lies in file Files[i].
	Locations []Location
}

// Blocks finds the duplicate blocks of the stream under opts, the same
// blocks Count counts, and returns the files and locations of each block's
// occurrences, in no particular order of blocks. Tokens added after the
// last EndFile are ignored.
func (s *Stream) Blocks(opts Options) ([]Block, error) {
	sa, reps, err := s.find(opts)
	if err != nil {
		return nil, err
	}
	total := 0
	for _, r := range reps {
		total += int(r.rb - r.lb + 1)
	}
	files := make([]int32, 0, total)
	locs := make([]Location, 0, total)
	out := make([]Block, 0, len(reps))
	for _, r := range reps {
		start := len(files)
		for _, p := range sa[r.lb : r.rb+1] {
			files = append(files, s.file[p])
			locs = append(locs, s.location(p, r.n))
		}
		end := len(files)
		out = append(out, Block{Tokens: int(r.n), Files: files[start:end:end], Locations: locs[start:end:end]})
	}
	return out, nil
}

// find returns the suffix array of the stream and its duplicate blocks
// under opts: the maximal repeats that survive merging, less the
// literal-only ones when opts.IgnoreLiteralOnly is set.
func (s *Stream) find(opts Options) (sa []int32, reps []repeat, err error) {
	if opts.MinTokens < 1 {
		return nil, nil, fmt.Errorf("finding duplicates: minimum of %d tokens is not positive", opts.MinTokens)
	}
	s.trim()
	sa, reps = find(s.codes, opts.MinTokens)
	if opts.IgnoreLiteralOnly {
		reps = s.dropLiteralOnly(sa, reps, opts.FoldSigns)
	}
	return sa, reps, nil
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
			res.Locations = append(res.Locations, s.location(p, b.n))
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

// location returns the location of the occurrence of n tokens starting at
// stream position p.
func (s *Stream) location(p, n int32) Location {
	return Location{
		File:      s.files[s.file[p]].name,
		StartLine: int(s.line[p]),
		EndLine:   int(s.last[p+n-1]),
	}
}

// Percent returns covered / sloc * 100 rounded to one decimal, or 0 when
// sloc is 0.
func Percent(covered, sloc int) float64 {
	if sloc <= 0 {
		return 0
	}
	return math.Round(float64(covered)/float64(sloc)*1000) / 10
}
