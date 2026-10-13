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
// a literal table next to code is kept whole. With Options.SplitLiteralRuns
// as well, each block is then cut at every run of at least MinTokens
// literal-only tokens, and the parts between the runs are kept as blocks
// when they are at least MinTokens long, so tables that declaration headers
// join into one block are dropped too. A part keeps every occurrence of
// its block, shifted by its offset; parts with the same first occurrence
// and length are one block. The language decides which tokens belong to
// which class; the rule itself is language-agnostic.
//
// Coverage. A line is covered when a token of any occurrence of any block
// starts or ends on it, or lies between, and counts only when the language
// marked it as a source line. duplicated_lines is the covered lines, and
// duplication_pct is covered lines over the package SLOC, times 100,
// rounded to one decimal.
//
// Anchors. Each block carries two anchors, the FNV-1a hashes of its first
// and of its last MinTokens normalized tokens, one key per token: the
// token's code, or the key the language gave it (Stream.AddKeyed) when its
// codes are interned per load. Equal sequences have equal anchors across
// loads, which is how a check tells a block the change added from one its
// baseline already had. Hashing the ends rather than the whole sequence
// keeps a block recognizable when a change moves one of its ends: a
// maximal repeat grows by a token when an edit makes the tokens after (or
// before) its occurrences agree, as appending a function after the last
// copy does, and shrinks when an edit cuts into one copy, while the other
// end, at least MinTokens long, stays as it was.
package duptok

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/rfizzle/astimate/internal/metrics"
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
	// SplitLiteralRuns is duplication.split_literal_runs: under
	// IgnoreLiteralOnly, cut each block at every run of at least MinTokens
	// literal-only tokens and keep the parts of at least MinTokens tokens.
	SplitLiteralRuns bool
}

// DefaultOptions returns the SPEC.md defaults: 40 tokens, literal-only
// blocks ignored, signed literals counted as literals, blocks not split at
// literal-only runs.
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
	// Code is dup_blocks_code: the blocks of Blocks whose kind is KindCode
	// (kind.go).
	Code int
	// Pct is duplication_pct: covered SLOC over package SLOC times 100,
	// rounded to one decimal.
	Pct float64
	// Lines is duplicated_lines: the covered SLOC, Pct's numerator.
	Lines int
	// Locations lists every occurrence of every block, block by block in
	// order of first occurrence, occurrences in stream order.
	Locations []Location
	// Tokens holds, for each entry of Locations, the length in tokens of
	// the block that occurrence belongs to.
	Tokens []int
	// Anchors holds, for each entry of Locations, the anchors of the block
	// that occurrence belongs to (Group.Anchors).
	Anchors [][2]uint64
	// Kinds holds, for each entry of Locations, the kind of the block that
	// occurrence belongs to.
	Kinds []Kind
}

// Group is one duplicate block of a Result with its occurrences.
type Group struct {
	// Anchors identify the block across loads: the hashes of the keys
	// (Stream.AddKeyed) of its first and of its last Options.MinTokens
	// tokens, so a block can be matched against the blocks of another
	// commit even when a change moved one of its ends (see the package
	// comment).
	Anchors [2]uint64
	// Kind is what the block is made of (kind.go).
	Kind Kind
	// Locations are the block's occurrences in stream order.
	Locations []Location
}

// LargestFirst returns r.Locations ordered by the length in tokens of
// their block, longest first, ties in the order of r.Locations (block by
// block in order of first occurrence, occurrences in stream order). The
// longest block is the copy worth extracting, so the first location is
// the one to cite. r is not modified.
func (r Result) LargestFirst() []Location {
	idx := r.largestFirst()
	out := make([]Location, len(idx))
	for i, j := range idx {
		out[i] = r.Locations[j]
	}
	return out
}

// Groups returns the blocks of r in the order of LargestFirst, each with
// its occurrences, so flattening them gives LargestFirst. Occurrences are
// grouped by anchors, so two blocks with the same anchors, as parts that
// Options.SplitLiteralRuns cuts from different blocks can be, form one
// group when they are adjacent in that order. r is not modified.
func (r Result) Groups() []Group {
	var out []Group
	for _, j := range r.largestFirst() {
		a := r.Anchors[j]
		if n := len(out); n == 0 || out[n-1].Anchors != a {
			out = append(out, Group{Anchors: a, Kind: r.kindOf(j)})
		}
		g := &out[len(out)-1]
		g.Locations = append(g.Locations, r.Locations[j])
	}
	return out
}

// Details returns the occurrences of groups in order, each rendered
// "file:start-end" with file passed through name (kept as it is when name
// is nil), and the groups as metrics.DupBlock with the same renderings, as
// metrics.Details lists them. Both are nil when groups is empty.
func Details(groups []Group, name func(string) string) ([]string, []metrics.DupBlock) {
	var locs []string
	var blocks []metrics.DupBlock
	for _, g := range groups {
		start := len(locs)
		for _, l := range g.Locations {
			file := l.File
			if name != nil {
				file = name(file)
			}
			locs = append(locs, file+":"+strconv.Itoa(l.StartLine)+"-"+strconv.Itoa(l.EndLine))
		}
		blocks = append(blocks, metrics.DupBlock{Anchors: g.Anchors, Kind: g.Kind.String(), Locations: slices.Clone(locs[start:])})
	}
	return locs, blocks
}

// kindOf returns the kind of the block of occurrence j, KindCode when r
// records no kinds.
func (r Result) kindOf(j int) Kind {
	if j < len(r.Kinds) {
		return r.Kinds[j]
	}
	return KindCode
}

// largestFirst returns the indexes of r.Locations in the order
// LargestFirst lists them.
func (r Result) largestFirst() []int {
	idx := make([]int, len(r.Locations))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, r.compareLength)
	return idx
}

// compareLength orders the occurrences i and j of r by block length in
// tokens, longer first. It is the comparator LargestFirst sorts with and
// the one a caller ranking blocks by another key first can wrap.
func (r Result) compareLength(i, j int) int {
	return cmp.Compare(r.Tokens[j], r.Tokens[i])
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
	codes []int32
	// keys holds each token's anchor key, nil while every token was added
	// with Add, whose key is its code.
	keys []uint64
	// shape holds each token's Shape, nil while none was set (SetShape).
	shape      []Shape
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
	s.grow(uint64(code))
	s.class = append(s.class, class)
	s.file = append(s.file, int32(len(s.files)))
	s.line = append(s.line, int32(line))
	s.last = append(s.last, int32(max(last, line)))
	return nil
}

// AddKeyed appends one token as Add does, with key standing for it in the
// anchors of a block (Group.Anchors) in place of its code. A
// language whose codes are interned per load, and so differ between the
// loads of two commits, passes a key that is the same in every load; a
// token added with Add has its code as its key.
func (s *Stream) AddKeyed(code int32, class Class, line, last int, key uint64) error {
	if err := s.Add(code, class, line, last); err != nil {
		return err
	}
	if s.keys == nil {
		s.keys = make([]uint64, len(s.codes), cap(s.codes))
		for i, c := range s.codes {
			s.keys[i] = uint64(c)
		}
	}
	s.keys[len(s.keys)-1] = key
	return nil
}

// grow extends the side slices in use, the keys with key and the shapes
// with ShapeCode, by the token just appended.
func (s *Stream) grow(key uint64) {
	if s.keys != nil {
		s.keys = append(s.keys, key)
	}
	if s.shape != nil {
		s.shape = append(s.shape, ShapeCode)
	}
}

// EndFile closes the current file under name, with code marking its source
// lines by 1-based index (index 0 is unused), and appends its separator.
func (s *Stream) EndFile(name string, code []bool) {
	fi := int32(len(s.files))
	s.files = append(s.files, file{name: name, code: code})
	s.codes = append(s.codes, SeparatorBase+fi)
	s.grow(uint64(SeparatorBase + fi))
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
	return s.count(sa, reps, sloc, opts.MinTokens), nil
}

// Block is one duplicate block as Blocks reports it.
type Block struct {
	// Tokens is the length of the block in tokens.
	Tokens int
	// Kind is what the block is made of (kind.go).
	Kind Kind
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
		pos := make([]int32, 0, r.rb-r.lb+1)
		for _, p := range sa[r.lb : r.rb+1] {
			files = append(files, s.file[p+r.off])
			locs = append(locs, s.location(p+r.off, r.n))
			pos = append(pos, p+r.off)
		}
		end := len(files)
		out = append(out, Block{Tokens: int(r.n), Kind: s.kind(pos, r.n), Files: files[start:end:end], Locations: locs[start:end:end]})
	}
	return out, nil
}

// find returns the suffix array of the stream and its duplicate blocks
// under opts: the maximal repeats that survive merging, less the
// literal-only ones when opts.IgnoreLiteralOnly is set, and cut at
// literal-only runs when opts.SplitLiteralRuns is set too.
func (s *Stream) find(opts Options) (sa []int32, reps []repeat, err error) {
	if opts.MinTokens < 1 {
		return nil, nil, fmt.Errorf("finding duplicates: minimum of %d tokens is not positive", opts.MinTokens)
	}
	s.trim()
	sa, reps = find(s.codes, opts.MinTokens)
	if opts.IgnoreLiteralOnly {
		reps = s.dropLiteralOnly(sa, reps, opts.FoldSigns)
		if opts.SplitLiteralRuns {
			reps = s.splitLiteralRuns(sa, reps, opts.FoldSigns, opts.MinTokens)
		}
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
	if s.keys != nil {
		s.keys = s.keys[:n]
	}
	if s.shape != nil {
		s.shape = s.shape[:n]
	}
	s.line, s.last = s.line[:n], s.last[:n]
}

// repeat is one duplicate block: its occurrences start at sa[lb..rb]
// shifted right by off tokens, and its length is n tokens. A maximal repeat
// has off 0; a part that splitLiteralRuns cuts from one has the part's
// offset in it.
type repeat struct {
	lb, rb, off, n int32
}

// literalOnly reports whether a token of class c belongs to a literal
// table: a literal or literal-table punctuation, or with signs a Sign.
func literalOnly(c Class, signs bool) bool {
	switch c {
	case Literal, Punct:
		return true
	case Sign:
		return signs
	default:
		return false
	}
}

// dropLiteralOnly returns reps without the repeats whose tokens are all
// literals or literal-table punctuation, reusing the backing array. With
// signs, a Sign token counts as a literal.
func (s *Stream) dropLiteralOnly(sa []int32, reps []repeat, signs bool) []repeat {
	return slices.DeleteFunc(reps, func(r repeat) bool {
		p := sa[r.lb] + r.off
		for _, c := range s.class[p : p+r.n] {
			if !literalOnly(c, signs) {
				return false
			}
		}
		return true
	})
}

// splitLiteralRuns cuts every block of reps at each run of at least
// minTokens literal-only tokens (with signs, Sign tokens count) and returns
// the parts of at least minTokens tokens, in the order of reps and of the
// parts within each. The token classes are read at one occurrence; codes,
// and so classes, are the same in all of them. A part whose first
// occurrence and length match an earlier part's is left out, so a
// sequence two blocks share after cutting counts once.
func (s *Stream) splitLiteralRuns(sa []int32, reps []repeat, signs bool, minTokens int) []repeat {
	type key struct{ first, n int32 }
	out := make([]repeat, 0, len(reps))
	seen := make(map[key]bool, len(reps))
	for _, r := range reps {
		p := sa[r.lb] + r.off
		first := slices.Min(sa[r.lb:r.rb+1]) + r.off
		from := int32(0)
		emit := func(to int32) {
			if int(to-from) < minTokens {
				return
			}
			if k := (key{first + from, to - from}); !seen[k] {
				seen[k] = true
				out = append(out, repeat{lb: r.lb, rb: r.rb, off: r.off + from, n: to - from})
			}
		}
		for i := int32(0); i < r.n; {
			if !literalOnly(s.class[p+i], signs) {
				i++
				continue
			}
			j := i
			for j < r.n && literalOnly(s.class[p+j], signs) {
				j++
			}
			if int(j-i) >= minTokens {
				emit(i)
				from = j
			}
			i = j
		}
		emit(r.n)
	}
	return out
}

// count turns the repeats found in the stream, as intervals of its suffix
// array sa, into block count, coverage percentage over sloc, and occurrence
// locations, with each block's anchors over minTokens tokens.
func (s *Stream) count(sa []int32, reps []repeat, sloc, minTokens int) Result {
	if len(reps) == 0 {
		return Result{}
	}
	// Blocks in order of first occurrence, occurrences in stream order.
	type block struct {
		n    int32
		pos  []int32
		kind Kind
	}
	blocks := make([]block, 0, len(reps))
	for _, r := range reps {
		pos := slices.Clone(sa[r.lb : r.rb+1])
		for i := range pos {
			pos[i] += r.off
		}
		slices.Sort(pos)
		blocks = append(blocks, block{n: r.n, pos: pos, kind: s.kind(pos, r.n)})
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
			res.Tokens = append(res.Tokens, int(b.n))
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
	res.Lines, res.Pct = lines, Percent(lines, sloc)
	res.Anchors = make([][2]uint64, 0, len(res.Locations))
	res.Kinds = make([]Kind, 0, len(res.Locations))
	for _, b := range blocks {
		a := s.anchors(b.pos[0], b.n, int32(minTokens))
		if b.kind == KindCode {
			res.Code++
		}
		for range b.pos {
			res.Anchors = append(res.Anchors, a)
			res.Kinds = append(res.Kinds, b.kind)
		}
	}
	return res
}

// anchors returns the anchors of the block of n tokens whose occurrence
// starts at stream position p: the hashes (hash) of its first and of its
// last k tokens, or of all n when n is shorter.
func (s *Stream) anchors(p, n, k int32) [2]uint64 {
	k = min(k, n)
	return [2]uint64{s.hash(p, k), s.hash(p+n-k, k)}
}

// hash returns the 64-bit FNV-1a hash, taken a key at a time, of the keys
// of the n tokens starting at stream position p.
func (s *Stream) hash(p, n int32) uint64 {
	const offset, prime = 14695981039346656037, 1099511628211
	h := uint64(offset)
	for i := p; i < p+n; i++ {
		k := uint64(s.codes[i])
		if s.keys != nil {
			k = s.keys[i]
		}
		h = (h ^ k) * prime
	}
	return h
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
