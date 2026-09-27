package golang

// Duplication (SPEC.md section 6.3).
//
// Stream. Every non-test file of the package is scanned with go/scanner,
// comments off, except generated files: those ast.IsGenerated reports, which
// carry a "// Code generated ... DO NOT EDIT." comment before the package
// clause, as Go's generated-file convention specifies. Each token becomes one
// int32 code:
//
//   - an identifier is token.IDENT, a string, char, int, float or imaginary
//     literal is token.INT (the single LIT code), so renamed copies match;
//   - with a normalization toggle off, identifiers (or literals) are instead
//     interned by text to codes from dupInternBase up, distinct per text;
//   - a keyword or operator keeps its token.Token value;
//   - an automatic semicolon (token.SEMICOLON with literal "\n") is dropped,
//     so a repeat never reaches onto the neighbouring declaration's line; an
//     explicit ";" is kept.
//
// The files are concatenated with one separator code after each file,
// dupSeparatorBase plus the file's index. Separators are unique in the
// stream and above every token and interned code, so no common prefix of two
// suffixes can run across a file boundary. Because every token is exactly one
// int32, a match can never start or end inside a token.
//
// Finder. A suffix array over the int32 stream is built by prefix doubling
// with counting sorts (O(n log n)), then the LCP array by Kasai's algorithm.
// index/suffixarray was considered and not used: it indexes bytes and only
// answers lookups, it does not expose the sorted suffixes or their common
// prefix lengths that maximal-repeat enumeration needs. A rolling hash would
// need verification and a separate maximality pass; the suffix array gives
// both exactly.
//
// A maximal repeat is a sequence occurring at least twice that cannot be
// extended left or right with all occurrences agreeing; in the suffix array
// it is an LCP interval whose occurrences are not all preceded by the same
// code. SPEC.md merging drops a maximal repeat each of whose occurrences lies
// inside an occurrence of a longer repeat (nested repeats, and the shorter
// periods of a tandem run); the survivors are the duplicate blocks.
//
// Enumerating every maximal repeat and testing containment occurrence by
// occurrence is quadratic on long periodic runs such as literal tables, so
// the survivors are found directly. Let L(q) be the longest repeat starting
// at stream position q (the larger LCP with q's two suffix-array
// neighbours). An occurrence [q, q+l) lies inside a longer repeat exactly
// when l < L(q) or some q' < q has q'+L(q') >= q+l, since any repeated
// segment extends to an occurrence of a maximal repeat at least as long. So,
// sweeping q upwards with reach = max(q'+L(q')) over q' < q, a position with
// L(q) >= minTokens and q+L(q) > reach starts an occurrence of a surviving
// block: the sequence of length L(q) at q, which is then left-maximal too
// (otherwise q-1 would reach as far). Its LCP interval, found from previous
// and next smaller LCP values, gives all its occurrences; blocks are the
// distinct intervals. Everything after the suffix array is linear. The
// occurrences of the blocks, overlapping or not, are unioned for coverage;
// every shorter repeat lies inside them, so the union is the same as over
// all repeats.
//
// Literal-only blocks. With dup_ignore_literal_only on, a surviving block
// whose every code is a literal (LIT, or an interned literal text) or one of
// the punctuation tokens , { } : [ ] ( ) and an explicit ; is dropped before
// counting, so a repeated run of a literal table (precomputed points, lookup
// tables) is neither a block nor coverage. Any identifier, keyword or other
// operator keeps the block. The rule runs after merging, so a block that
// holds a literal table next to code is kept whole.
//
// Signed literals. The scanner records each + or - that directly precedes
// an int, float, imaginary or char literal and follows a token that cannot
// end an operand (anything but an identifier, a literal, or ) ] }), which
// is to say a unary sign. With dup_fold_signs on, the literal-only rule
// counts such a sign as part of its literal, so a table of negative numbers
// is dropped too. The stream keeps the sign as its own code: folding it
// into the literal there was measured and rejected, because it lets f(-1)
// match f(1), which merges signed coefficient tables with the code around
// them, and it shortens code blocks below dup_min_tokens.
//
// Coverage. A line is covered when it holds a code byte of a token in any
// occurrence of any block, and counts only if it is a source line by the
// same rule size uses: at least one non-space byte outside comments.
// duplication_pct is covered lines over the package SLOC from size (which
// includes generated files), times 100, rounded to one decimal.

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/scanner"
	"go/token"
	"math"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

const (
	// dupIdentCode is the normalized code of every identifier (ID).
	dupIdentCode = int32(token.IDENT)
	// dupLitCode is the normalized code of every basic literal (LIT).
	dupLitCode = int32(token.INT)
	// dupInternBase is the first code given to an interned identifier or
	// literal text when its normalization is off. It is above every
	// token.Token value.
	dupInternBase = int32(1 << 12)
	// dupSeparatorBase is the code of the separator after the first file;
	// the separator after file i is dupSeparatorBase + i.
	dupSeparatorBase = int32(1 << 30)
)

// dupOptions configures duplication. The extractor options WithDupMinTokens,
// WithDupIgnoreLiteralOnly and WithDupFoldSigns set minTokens,
// ignoreLiteralOnly and foldSigns; the normalization toggles keep their
// defaults.
type dupOptions struct {
	// minTokens is dup_min_tokens: the shortest normalized token sequence
	// that counts as a duplicate block.
	minTokens int
	// normalizeIdents maps every identifier to one code.
	normalizeIdents bool
	// normalizeLiterals maps every string, char and numeric literal to one
	// code.
	normalizeLiterals bool
	// ignoreLiteralOnly is dup_ignore_literal_only: drop a block made only
	// of literals and punctuation.
	ignoreLiteralOnly bool
	// foldSigns is dup_fold_signs: under ignoreLiteralOnly, a unary + or -
	// directly before a numeric literal counts as part of the literal. The
	// stream itself is unchanged, so no match is gained or lost.
	foldSigns bool
}

// defaultDupOptions returns the SPEC.md defaults: 40 tokens, identifiers and
// literals normalized, literal-only blocks ignored with signed literals
// counted as literals.
func defaultDupOptions() dupOptions {
	return dupOptions{minTokens: 40, normalizeIdents: true, normalizeLiterals: true, ignoreLiteralOnly: true, foldSigns: true}
}

// dupCounts holds the duplication metrics of one package.
type dupCounts struct {
	// blocks is dup_blocks: distinct maximal repeated sequences after
	// merging.
	blocks int
	// pct is duplication_pct: covered SLOC over package SLOC times 100,
	// rounded to one decimal.
	pct float64
	// locations lists every occurrence of every block, block by block in
	// order of first occurrence, for suggestions and debugging.
	locations []dupLocation
}

// dupLocation is one occurrence of a duplicate block: the absolute filename
// and the first and last lines holding its tokens.
type dupLocation struct {
	file               string
	startLine, endLine int
}

// dupFile is one scanned file of the stream.
type dupFile struct {
	name string
	// code reports, per 1-based line, whether the line holds a non-space
	// byte of some token, which is the SLOC rule size applies.
	code []bool
}

// dupStream is the normalized token stream of a package: codes, and for each
// token its file index and first and last line. Separators have file -1.
type dupStream struct {
	codes      []int32
	file       []int32
	line, last []int32
	files      []dupFile
	// intern maps identifier or literal text to its code when a
	// normalization toggle is off.
	intern map[string]int32
	// internLit reports, per interned code minus dupInternBase, whether the
	// text is a literal.
	internLit []bool
	// signs holds, in increasing order, the stream positions of the unary
	// + and - tokens directly before a numeric literal.
	signs []int32
}

// dupRepeat is one maximal repeat: its occurrences are sa[lb..rb] and its
// length is n tokens.
type dupRepeat struct {
	lb, rb, n int32
}

// duplication computes dup_blocks and duplication_pct for p from its
// non-test, non-generated files, read through src, with sz the size metrics
// of p (its sloc is the denominator of the percentage). See the file comment
// for the algorithm.
func duplication(l *loaded, p *packages.Package, src fileSource, sz sizeCounts, opts dupOptions) (dupCounts, error) {
	if opts.minTokens < 1 {
		return dupCounts{}, fmt.Errorf("detecting duplication in %s: minimum of %d tokens is not positive", p.PkgPath, opts.minTokens)
	}
	s, err := dupStreamOf(l, p, src, opts)
	if err != nil {
		return dupCounts{}, fmt.Errorf("detecting duplication in %s: %w", p.PkgPath, err)
	}
	sa, reps := s.find(opts)
	return s.count(sa, reps, sz.sloc), nil
}

// dupStreamOf scans the non-test, non-generated files of p, read through
// src, into one normalized stream under opts.
func dupStreamOf(l *loaded, p *packages.Package, src fileSource, opts dupOptions) (*dupStream, error) {
	s := &dupStream{intern: make(map[string]int32)}
	fs := token.NewFileSet()
	for _, f := range sourceSyntax(l, p) {
		if ast.IsGenerated(f) {
			continue
		}
		tf := l.fset.File(f.FileStart)
		if tf == nil {
			return nil, errors.New("file not in file set")
		}
		data, err := src.read(tf.Name())
		if err != nil {
			return nil, err
		}
		if err := s.scan(fs, tf.Name(), data, opts); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// find returns the suffix array of the stream and its duplicate blocks
// under opts: the maximal repeats that survive merging, less the
// literal-only ones when opts.ignoreLiteralOnly is set.
func (s *dupStream) find(opts dupOptions) (sa []int32, reps []dupRepeat) {
	sa, reps = dupFind(s.codes, opts.minTokens)
	if opts.ignoreLiteralOnly {
		reps = s.dropLiteralOnly(sa, reps, opts.foldSigns)
	}
	return sa, reps
}

// scan appends the normalized tokens of one file, then its separator.
func (s *dupStream) scan(fs *token.FileSet, name string, src []byte, opts dupOptions) error {
	tf := fs.AddFile(name, -1, len(src))
	var sc scanner.Scanner
	var scanErr error
	sc.Init(tf, src, func(pos token.Position, msg string) {
		if scanErr == nil {
			scanErr = fmt.Errorf("scanning %s: %s", pos, msg)
		}
	}, 0)
	fi := int32(len(s.files))
	df := dupFile{name: name, code: make([]bool, bytes.Count(src, []byte{'\n'})+2)}
	// operand: the previous token can end an operand; unary: the previous
	// token is a + or - that does not follow one.
	operand, unary := false, false
	for {
		pos, tok, lit := sc.Scan()
		if tok == token.EOF {
			break
		}
		if unary && isNumericLit(tok) {
			s.signs = append(s.signs, int32(len(s.codes)-1))
		}
		unary = !operand && (tok == token.SUB || tok == token.ADD)
		operand = endsOperand(tok)
		if tok == token.SEMICOLON && lit == "\n" {
			continue
		}
		line := int32(tf.Line(pos))
		last := line
		df.code[line] = true
		if tok == token.STRING && strings.IndexByte(lit, '\n') >= 0 {
			last = markRawLines(df.code, line, lit)
		}
		s.codes = append(s.codes, s.code(tok, lit, opts))
		s.file = append(s.file, fi)
		s.line = append(s.line, line)
		s.last = append(s.last, last)
	}
	if scanErr != nil {
		return scanErr
	}
	s.files = append(s.files, df)
	s.codes = append(s.codes, dupSeparatorBase+fi)
	s.file = append(s.file, -1)
	s.line = append(s.line, 0)
	s.last = append(s.last, 0)
	return nil
}

// endsOperand reports whether tok can be the last token of an operand, so
// that a + or - after it is binary.
func endsOperand(tok token.Token) bool {
	switch tok {
	case token.IDENT, token.RPAREN, token.RBRACK, token.RBRACE:
		return true
	}
	return tok.IsLiteral()
}

// isNumericLit reports whether tok is an int, float, imaginary or char
// literal, the literals a unary sign applies to.
func isNumericLit(tok token.Token) bool {
	switch tok {
	case token.INT, token.FLOAT, token.IMAG, token.CHAR:
		return true
	}
	return false
}

// markRawLines marks the lines after line that the multi-line raw string lit
// holds a non-space byte on, and returns the literal's last line.
func markRawLines(code []bool, line int32, lit string) int32 {
	for i := 0; i < len(lit); i++ {
		switch b := lit[i]; {
		case b == '\n':
			line++
		case !isSpace(b):
			code[line] = true
		}
	}
	return line
}

// code returns the stream code of one token under opts.
func (s *dupStream) code(tok token.Token, lit string, opts dupOptions) int32 {
	switch {
	case tok == token.IDENT:
		if opts.normalizeIdents {
			return dupIdentCode
		}
		return s.interned("i" + lit)
	case tok.IsLiteral():
		if opts.normalizeLiterals {
			return dupLitCode
		}
		return s.interned("l" + lit)
	default:
		return int32(tok)
	}
}

// interned returns the code of key, assigning the next free one on first
// use.
func (s *dupStream) interned(key string) int32 {
	c, ok := s.intern[key]
	if !ok {
		c = dupInternBase + int32(len(s.intern))
		s.intern[key] = c
		s.internLit = append(s.internLit, key[0] == 'l')
	}
	return c
}

// dropLiteralOnly returns reps without the repeats whose codes are all
// literals or literal-table punctuation, reusing the backing array. With
// signs, a unary + or - directly before a numeric literal counts as part of
// the literal.
func (s *dupStream) dropLiteralOnly(sa []int32, reps []dupRepeat, signs bool) []dupRepeat {
	return slices.DeleteFunc(reps, func(r dupRepeat) bool {
		p := sa[r.lb]
		for i, c := range s.codes[p : p+r.n] {
			if s.literalOrPunct(c) {
				continue
			}
			if !signs || (c != int32(token.SUB) && c != int32(token.ADD)) {
				return false
			}
			if _, ok := slices.BinarySearch(s.signs, p+int32(i)); !ok {
				return false
			}
		}
		return true
	})
}

// literalOrPunct reports whether stream code c is a literal or one of the
// punctuation tokens a literal table is written with.
func (s *dupStream) literalOrPunct(c int32) bool {
	switch {
	case c == dupLitCode:
		return true
	case c >= dupInternBase && c < dupSeparatorBase:
		return s.internLit[c-dupInternBase]
	}
	switch token.Token(c) {
	case token.COMMA, token.LBRACE, token.RBRACE, token.COLON,
		token.LBRACK, token.RBRACK, token.LPAREN, token.RPAREN, token.SEMICOLON:
		return true
	}
	return false
}

// count turns the repeats found in the stream, as intervals of its suffix
// array sa, into block count, coverage percentage over sloc, and occurrence
// locations.
func (s *dupStream) count(sa []int32, reps []dupRepeat, sloc int) dupCounts {
	if len(reps) == 0 {
		return dupCounts{}
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
	c := dupCounts{blocks: len(blocks)}
	for _, b := range blocks {
		for _, p := range b.pos {
			diff[p]++
			diff[p+b.n]--
			end := p + b.n - 1
			c.locations = append(c.locations, dupLocation{
				file:      s.files[s.file[p]].name,
				startLine: int(s.line[p]),
				endLine:   int(s.last[end]),
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
		for ln := s.line[t]; ln <= s.last[t]; ln++ {
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
	c.pct = dupPercent(lines, sloc)
	return c
}

// dupPercent returns covered / sloc * 100 rounded to one decimal, or 0 when
// sloc is 0.
func dupPercent(covered, sloc int) float64 {
	if sloc <= 0 {
		return 0
	}
	return math.Round(float64(covered)/float64(sloc)*1000) / 10
}

// dupFind returns the suffix array of s and the maximal repeats of at least
// minTokens codes in s that survive merging, as intervals of that suffix
// array. s must end with a code that occurs nowhere else in it.
func dupFind(s []int32, minTokens int) (sa []int32, reps []dupRepeat) {
	n := len(s)
	if n < 2 {
		return nil, nil
	}
	sa, rank := dupSuffixArray(s)
	lcp := dupLCP(s, sa, rank)
	prevLess, nextLess := dupSmallerNeighbours(lcp)
	lcpAt := func(k int32) int32 {
		if int(k) < n {
			return lcp[k]
		}
		return 0
	}
	type key struct{ lb, n int32 }
	seen := make(map[key]bool)
	reach := int32(0)
	for q := range int32(n) {
		r := rank[q]
		longest := max(lcp[r], lcpAt(r+1))
		if int(longest) >= minTokens && q+longest > reach {
			lb, rb := r, r
			if lcp[r] == longest {
				lb = prevLess[r]
			}
			if lcpAt(r+1) == longest {
				rb = nextLess[r+1] - 1
			}
			if k := (key{lb, longest}); !seen[k] {
				seen[k] = true
				reps = append(reps, dupRepeat{lb: lb, rb: rb, n: longest})
			}
		}
		reach = max(reach, q+longest)
	}
	return sa, reps
}

// dupSmallerNeighbours returns, for each index k of lcp, the largest j < k
// and the smallest j > k with lcp[j] < lcp[k]; -1 and len(lcp) when there is
// none.
func dupSmallerNeighbours(lcp []int32) (prevLess, nextLess []int32) {
	n := len(lcp)
	prevLess = make([]int32, n)
	nextLess = make([]int32, n)
	stack := make([]int32, 0, 64)
	for k := range int32(n) {
		for len(stack) > 0 && lcp[stack[len(stack)-1]] >= lcp[k] {
			stack = stack[:len(stack)-1]
		}
		prevLess[k] = -1
		if len(stack) > 0 {
			prevLess[k] = stack[len(stack)-1]
		}
		stack = append(stack, k)
	}
	stack = stack[:0]
	for k := int32(n) - 1; k >= 0; k-- {
		for len(stack) > 0 && lcp[stack[len(stack)-1]] >= lcp[k] {
			stack = stack[:len(stack)-1]
		}
		nextLess[k] = int32(n)
		if len(stack) > 0 {
			nextLess[k] = stack[len(stack)-1]
		}
		stack = append(stack, k)
	}
	return prevLess, nextLess
}

// dupSuffixArray returns the suffix array of s and its inverse, built by
// prefix doubling with counting sorts. s must end with a unique code.
func dupSuffixArray(s []int32) (sa, rank []int32) {
	n := len(s)
	sa = make([]int32, n)
	rank = make([]int32, n)
	for i := range sa {
		sa[i] = int32(i)
	}
	slices.SortFunc(sa, func(a, b int32) int { return cmp.Compare(s[a], s[b]) })
	for i := 1; i < n; i++ {
		rank[sa[i]] = rank[sa[i-1]]
		if s[sa[i]] != s[sa[i-1]] {
			rank[sa[i]]++
		}
	}
	if n == 0 || int(rank[sa[n-1]]) == n-1 {
		return sa, rank
	}
	tmp := make([]int32, n)
	by2 := make([]int32, n)
	cnt := make([]int32, n+1)
	for k := 1; ; k <<= 1 {
		// Order by second key: suffixes with no second half first.
		j := 0
		for i := n - k; i < n; i++ {
			by2[j] = int32(i)
			j++
		}
		for _, v := range sa {
			if int(v) >= k {
				by2[j] = v - int32(k)
				j++
			}
		}
		// Stable counting sort by first key.
		clear(cnt)
		for _, r := range rank {
			cnt[r+1]++
		}
		for i := 1; i <= n; i++ {
			cnt[i] += cnt[i-1]
		}
		for _, v := range by2 {
			sa[cnt[rank[v]]] = v
			cnt[rank[v]]++
		}
		second := func(i int32) int32 {
			if int(i)+k < n {
				return rank[int(i)+k]
			}
			return -1
		}
		tmp[sa[0]] = 0
		for i := 1; i < n; i++ {
			a, b := sa[i-1], sa[i]
			tmp[b] = tmp[a]
			if rank[a] != rank[b] || second(a) != second(b) {
				tmp[b]++
			}
		}
		rank, tmp = tmp, rank
		if int(rank[sa[n-1]]) == n-1 {
			return sa, rank
		}
	}
}

// dupLCP returns lcp where lcp[i] is the length of the longest common prefix
// of the suffixes sa[i-1] and sa[i], and lcp[0] is 0 (Kasai et al.).
func dupLCP(s, sa, rank []int32) []int32 {
	n := len(s)
	lcp := make([]int32, n)
	h := 0
	for i := range n {
		r := rank[i]
		if r == 0 {
			h = 0
			continue
		}
		j := int(sa[r-1])
		for i+h < n && j+h < n && s[i+h] == s[j+h] {
			h++
		}
		lcp[r] = int32(h)
		if h > 0 {
			h--
		}
	}
	return lcp
}
