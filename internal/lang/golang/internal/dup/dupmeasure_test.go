package dup

import (
	"cmp"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/rfizzle/astimate/internal/lang/duptok"
	"github.com/rfizzle/astimate/internal/lang/duptok/duptoktest"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/inspect"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
)

// The variant measurements below rebuild rejected or pending refinements of
// the duplicate-block rules over the duptok finder, through the test-only
// recorder in duptoktest, for the notes in calibration/notes. They load
// whole module sets, so each runs only when its environment variable is
// set and make check skips them.

// dupMeasurePkg is one package of a variant measurement with the SLOC
// denominator its percentages use.
type dupMeasurePkg struct {
	p    *packages.Package
	sloc int
}

// dupMeasureLoad loads pattern in dir (the working directory when empty)
// with NeedName | NeedFiles | NeedSyntax and returns the packages without
// load errors and with at least 200 SLOC. With withGenerated the SLOC
// counts generated files too, as size did before generated files left the
// size metrics, which is the denominator the std notes were measured with;
// otherwise it is size's authored SLOC, the extractor's current one. It
// logs how many packages were loaded and how many load errors skipped.
func dupMeasureLoad(t *testing.T, dir, pattern string, withGenerated bool) (*load.Module, []dupMeasurePkg) {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedFiles | packages.NeedSyntax | packages.NeedName,
		Fset: fset,
		Dir:  dir,
	}, pattern)
	if err != nil {
		t.Fatalf("loading %s in %q: %v", pattern, dir, err)
	}
	l := &load.Module{Fset: fset}
	out := make([]dupMeasurePkg, 0, len(pkgs))
	errored := 0
	defer func() { t.Logf("loaded %d packages, %d skipped for load errors", len(pkgs), errored) }()
	for _, p := range pkgs {
		if len(p.Errors) > 0 {
			errored++
			continue
		}
		sz, err := inspect.Size(l, p, load.OSFiles{})
		if err != nil {
			t.Fatalf("size %s: %v", p.PkgPath, err)
		}
		sloc := sz.SLOC
		if withGenerated {
			sloc = dupSLOCWithGenerated(t, l, p)
		}
		if sloc < 200 {
			continue
		}
		out = append(out, dupMeasurePkg{p: p, sloc: sloc})
	}
	return l, out
}

// dupSLOCWithGenerated returns the SLOC of every non-test file of p,
// generated files included.
func dupSLOCWithGenerated(t *testing.T, l *load.Module, p *packages.Package) int {
	t.Helper()
	n := 0
	for _, f := range l.SourceSyntax(p) {
		tf := l.Fset.File(f.FileStart)
		if tf == nil {
			t.Fatalf("%s: file not in file set", p.PkgPath)
		}
		data, err := load.OSFiles{}.Read(tf.Name())
		if err != nil {
			t.Fatalf("reading %s: %v", tf.Name(), err)
		}
		n += inspect.FileSLOC(tf, f, data)
	}
	return n
}

// dupStdWithGenerated reports whether the std measurements use the SLOC
// denominator their notes were recorded with (generated files counted),
// which is the default; ASTIMATE_MEASURE_SLOC=authored switches them to the
// extractor's current, authored-only denominator.
func dupStdWithGenerated() bool {
	return os.Getenv("ASTIMATE_MEASURE_SLOC") != "authored"
}

// dupRecord scans the authored non-test files of p under opts into a
// recorder, the stream duplication builds.
func dupRecord(t *testing.T, l *load.Module, p *packages.Package, opts Options) *duptoktest.Recorder {
	t.Helper()
	r := &duptoktest.Recorder{}
	if err := newDupTokenizer(opts).appendPackage(token.NewFileSet(), l, p, load.OSFiles{}, r); err != nil {
		t.Fatalf("scanning %s: %v", p.PkgPath, err)
	}
	return r
}

// dupRecordBlocks returns the blocks of r under opts.
func dupRecordBlocks(t *testing.T, r *duptoktest.Recorder, opts Options) []duptoktest.Block {
	t.Helper()
	blocks, err := r.Blocks(opts.finder())
	if err != nil {
		t.Fatal(err)
	}
	return blocks
}

// dupCheckBaseline fails t when base, counted over the recorder, differs
// from what duplication reports for p under opts with the same
// denominator, so a variant is always measured against the shipped rule.
func dupCheckBaseline(t *testing.T, l *load.Module, m dupMeasurePkg, opts Options, base duptok.Result) {
	t.Helper()
	want, err := Package(l, m.p, load.OSFiles{}, m.sloc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if base.Blocks != want.Blocks || base.Pct != want.Pct || !slices.Equal(base.Locations, want.Locations) {
		t.Errorf("%s: recorder baseline %d blocks %v%%, duplication %d blocks %v%%",
			m.p.PkgPath, base.Blocks, base.Pct, want.Blocks, want.Pct)
	}
}

// TestDupMeasureStdlibRefinements measures two candidate refinements of the
// duplicate-block rules against the shipped defaults over the standard
// library, for calibration/notes/duplication-refinements.md. A1 folds each
// unary sign before a numeric literal into the literal in the stream; A2
// (dup_fold_signs) leaves the stream alone and lets the literal-only rule
// count such a sign as literal; B drops a block whose only non-literal
// tokens are calls to one or two distinct functions (strictly, every
// identifier is a callee; loosely, identifiers may also be whole
// arguments). It loads every std package, so it runs only with
// ASTIMATE_MEASURE_STDLIB=1.
func TestDupMeasureStdlibRefinements(t *testing.T) {
	if os.Getenv("ASTIMATE_MEASURE_STDLIB") != "1" {
		t.Skip("set ASTIMATE_MEASURE_STDLIB=1 to measure the standard library")
	}
	start := time.Now()
	l, pkgs := dupMeasureLoad(t, "", "std", dupStdWithGenerated())
	base := DefaultOptions()
	base.FoldSigns = false // the defaults before dup_fold_signs
	signs := base
	signs.FoldSigns = true
	raw := base
	raw.NormalizeIdents = false
	variants := []string{"A1 stream fold", "A2 sign-aware literal-only", "B strict", "B loose"}
	rows := make([][]dupMeasureRow, len(variants))
	changes := make([][]string, len(variants))
	for _, m := range pkgs {
		p := m.p
		s := dupRecord(t, l, p, base)
		bb := dupRecordBlocks(t, s, base)
		b, _ := s.Count(bb, m.sloc)
		dupCheckBaseline(t, l, m, base, b)
		f := dupFoldSigns(s)
		a1, _ := f.Count(dupRecordBlocks(t, f, base), m.sloc)
		a2, _ := s.Count(dupRecordBlocks(t, s, signs), m.sloc)
		for v, a := range []duptok.Result{a1, a2} {
			rows[v] = append(rows[v], dupMeasureRow{p.PkgPath, m.sloc, b.Blocks, a.Blocks, b.Pct, a.Pct})
			changes[v] = append(changes[v], dupLocDiff(p.PkgPath, "-", b.Locations, a.Locations)...)
			changes[v] = append(changes[v], dupLocDiff(p.PkgPath, "+", a.Locations, b.Locations)...)
		}
		names := dupRecord(t, l, p, raw)
		for i, loose := range []bool{false, true} {
			v := i + 2
			kept := slices.DeleteFunc(slices.Clone(bb), func(blk duptoktest.Block) bool {
				at := blk.Lead
				if !dupCallChain(s, names, at, blk.N, loose) {
					return false
				}
				loc := s.Location(at, blk.N)
				changes[v] = append(changes[v], "- "+p.PkgPath+" "+loc.File+":"+
					strconv.Itoa(loc.StartLine)+"-"+strconv.Itoa(loc.EndLine)+
					" x"+strconv.Itoa(len(blk.Pos))+" n"+strconv.Itoa(int(blk.N)))
				return true
			})
			c, _ := s.Count(kept, m.sloc)
			rows[v] = append(rows[v], dupMeasureRow{p.PkgPath, m.sloc, b.Blocks, c.Blocks, b.Pct, c.Pct})
		}
	}
	t.Logf("packages %d, wall %v", len(rows[0]), time.Since(start))
	for v, name := range variants {
		t.Logf("=== %s (off = shipped defaults, on = variant)", name)
		dupLogMeasure(t, rows[v])
		t.Logf("changed block occurrences (%d):", len(changes[v]))
		for _, c := range changes[v] {
			t.Log(c)
		}
	}
}

// dupLogMeasure logs the quantiles, totals, named packages, largest drops
// and rises and top ten of one variant's rows.
func dupLogMeasure(t *testing.T, rows []dupMeasureRow) {
	t.Helper()
	offPct := func(r dupMeasureRow) float64 { return r.offPct }
	onPct := func(r dupMeasureRow) float64 { return r.onPct }
	offB := func(r dupMeasureRow) float64 { return float64(r.offB) }
	onB := func(r dupMeasureRow) float64 { return float64(r.onB) }
	for _, q := range []float64{0.5, 0.9, 0.99} {
		t.Logf("p%v: duplication_pct off %v on %v; dup_blocks off %v on %v", q*100,
			dupQuantile(rows, offPct, q), dupQuantile(rows, onPct, q),
			dupQuantile(rows, offB, q), dupQuantile(rows, onB, q))
	}
	totalOff, totalOn, changed := 0, 0, 0
	for _, r := range rows {
		totalOff += r.offB
		totalOn += r.onB
		if r.offB != r.onB || r.offPct != r.onPct {
			changed++
		}
	}
	t.Logf("total dup_blocks off %d on %d; packages changed %d", totalOff, totalOn, changed)
	row := func(r dupMeasureRow) {
		t.Logf("| %s | %d | %d | %d | %v | %v |", r.path, r.sloc, r.offB, r.onB, r.offPct, r.onPct)
	}
	t.Log("named:")
	for _, r := range rows {
		switch r.path {
		case "crypto/internal/fips140/nistec", "math/big", "net/http":
			row(r)
		}
	}
	top := func(title string, get func(dupMeasureRow) float64) {
		t.Log(title)
		s := slices.Clone(rows)
		slices.SortStableFunc(s, func(a, b dupMeasureRow) int { return cmp.Compare(get(b), get(a)) })
		for _, r := range s[:min(10, len(s))] {
			row(r)
		}
	}
	top("largest drop:", func(r dupMeasureRow) float64 { return r.offPct - r.onPct })
	top("largest rise:", func(r dupMeasureRow) float64 { return r.onPct - r.offPct })
	top("top ten on:", onPct)
}

// dupFoldSigns returns a copy of s with every unary sign before a numeric
// literal (a token of class duptok.Sign) removed from the stream, so the
// literal's code stands for the signed number: the fold considered in the
// normalizer and not shipped. The literal takes the sign's line.
func dupFoldSigns(s *duptoktest.Recorder) *duptoktest.Recorder {
	f := &duptoktest.Recorder{Tokens: make([]duptoktest.Token, 0, len(s.Tokens)), Files: s.Files}
	for i, tk := range s.Tokens {
		if tk.Class == duptok.Sign {
			continue
		}
		if i > 0 && s.Tokens[i-1].Class == duptok.Sign {
			tk.Line = s.Tokens[i-1].Line
		}
		f.Tokens = append(f.Tokens, tk)
	}
	return f
}

// dupLocDiff returns, prefixed by mark and pkg, the locations in a that are
// not in b.
func dupLocDiff(pkg, mark string, a, b []duptok.Location) []string {
	var out []string
	for _, x := range a {
		if !slices.Contains(b, x) {
			out = append(out, mark+" "+pkg+" "+x.File+":"+strconv.Itoa(x.StartLine)+"-"+strconv.Itoa(x.EndLine))
		}
	}
	return out
}

// dupCallChain reports whether the n tokens of s at p are a call chain:
// every token is a literal, table punctuation or an identifier; the
// identifiers followed by "(" (the callees) have one or two distinct texts
// in names, the parallel stream with identifiers interned; and every other
// identifier is, when loose, a whole argument between "(" or "," and ","
// or ")", and when not loose, absent.
func dupCallChain(s, names *duptoktest.Recorder, p, n int32, loose bool) bool {
	callees := make([]int32, 0, 2)
	toks := s.Tokens[p : p+n]
	for i, tk := range toks {
		if tk.Class == duptok.Literal || tk.Class == duptok.Punct {
			continue
		}
		if tk.Code != dupIdentCode {
			return false
		}
		next, prev := token.ILLEGAL, token.ILLEGAL
		if i+1 < len(toks) {
			next = token.Token(toks[i+1].Code)
		}
		if i > 0 {
			prev = token.Token(toks[i-1].Code)
		}
		if next == token.LPAREN {
			if name := names.Tokens[int(p)+i].Code; !slices.Contains(callees, name) {
				callees = append(callees, name)
			}
			continue
		}
		if !loose || (prev != token.LPAREN && prev != token.COMMA) || (next != token.COMMA && next != token.RPAREN) {
			return false
		}
	}
	return len(callees) > 0 && len(callees) <= 2
}

// TestDupMeasureStdlibLiteralRuns measures two ways of reaching literal
// tables inside mixed duplicate blocks against the shipped defaults over the
// standard library, for calibration/notes/duplication-literal-runs.md; see
// dupMeasureLiteralRuns. It loads every std package, so it runs only with
// ASTIMATE_MEASURE_STDLIB=1.
func TestDupMeasureStdlibLiteralRuns(t *testing.T) {
	if os.Getenv("ASTIMATE_MEASURE_STDLIB") != "1" {
		t.Skip("set ASTIMATE_MEASURE_STDLIB=1 to measure the standard library")
	}
	start := time.Now()
	l, pkgs := dupMeasureLoad(t, "", "std", dupStdWithGenerated())
	dupMeasureLiteralRuns(t, l, pkgs, start)
}

// TestDupMeasureModuleLiteralRuns is TestDupMeasureStdlibLiteralRuns over
// every package of the module rooted at ASTIMATE_MEASURE_MODULE, with the
// extractor's authored SLOC as the denominator, for measuring the variants
// on a corpus beyond std. It runs only when that variable is set.
func TestDupMeasureModuleLiteralRuns(t *testing.T) {
	root := os.Getenv("ASTIMATE_MEASURE_MODULE")
	if root == "" {
		t.Skip("set ASTIMATE_MEASURE_MODULE to a module root to measure it")
	}
	start := time.Now()
	l, pkgs := dupMeasureLoad(t, root, "./...", false)
	dupMeasureLiteralRuns(t, l, pkgs, start)
}

// dupMeasureLiteralRuns applies the literal-run variants to the shipped
// blocks of every package of pkgs, loaded into l, and logs the results. T
// trims literal-only prefixes and suffixes off each block and drops what is
// left below duplication.min_tokens; S splits each block at every
// literal-only run of at least duplication.min_tokens tokens and keeps the
// parts of at least that length; S10 is S with runs of at least 10 tokens,
// short enough to reach the coefficient tables in math. Literal-only is the
// sign-aware rule: a token of class Literal, Punct or Sign. S is checked
// against duplication with split_literal_runs on, package by package. With
// ASTIMATE_MEASURE_ROWS=1 every package row of every variant is logged, for
// pooling quantiles across modules. start is when loading began, for the
// wall time.
func dupMeasureLiteralRuns(t *testing.T, l *load.Module, pkgs []dupMeasurePkg, start time.Time) {
	t.Helper()
	opts := DefaultOptions()
	variants := []string{"T trim", "S split", "S10 split at runs of 10"}
	rows := make([][]dupMeasureRow, len(variants))
	changes := make([][]string, len(variants))
	mathFiles := []string{"j0.go", "j1.go", "erf.go", "lgamma.go"}
	mathLines := make([]map[string]int, len(variants)+1)
	for _, m := range pkgs {
		p := m.p
		s := dupRecord(t, l, p, opts)
		bb := dupRecordBlocks(t, s, opts)
		b, bl := s.Count(bb, m.sloc)
		dupCheckBaseline(t, l, m, opts, b)
		if p.PkgPath == "math" {
			mathLines[0] = bl
		}
		for v, blocks := range [][]duptoktest.Block{
			dupTrimRuns(s, bb, opts.MinTokens),
			dupSplitRuns(s, bb, opts.MinTokens, opts.MinTokens),
			dupSplitRuns(s, bb, 10, opts.MinTokens),
		} {
			a, al := s.Count(blocks, m.sloc)
			if p.PkgPath == "math" {
				mathLines[v+1] = al
			}
			if v == 1 {
				dupCheckSplit(t, l, m, opts, a)
			}
			rows[v] = append(rows[v], dupMeasureRow{p.PkgPath, m.sloc, b.Blocks, a.Blocks, b.Pct, a.Pct})
			changes[v] = append(changes[v], dupOccDiff(s, p.PkgPath, "-", bb, blocks)...)
			changes[v] = append(changes[v], dupOccDiff(s, p.PkgPath, "+", blocks, bb)...)
		}
	}
	t.Logf("packages %d, wall %v", len(rows[0]), time.Since(start))
	t.Log("math covered lines per file: base, T, S, S10")
	for _, f := range mathFiles {
		var got []string
		for _, m := range mathLines {
			for name, n := range m {
				if filepath.Base(name) == f {
					got = append(got, strconv.Itoa(n))
				}
			}
		}
		t.Logf("| %s | %s |", f, strings.Join(got, " | "))
	}
	for v, name := range variants {
		t.Logf("=== %s (off = shipped defaults, on = variant)", name)
		dupLogMeasure(t, rows[v])
		named := []string{"math", "math/big", "math/rand", "math/cmplx"}
		for _, r := range rows[v] {
			if slices.Contains(named, r.path) {
				t.Logf("| %s | %d | %d | %d | %v | %v |", r.path, r.sloc, r.offB, r.onB, r.offPct, r.onPct)
			}
		}
		if os.Getenv("ASTIMATE_MEASURE_ROWS") == "1" {
			// Every row, for pooling quantiles across modules.
			for _, r := range rows[v] {
				t.Logf("row\t%d\t%s\t%d\t%d\t%d\t%v\t%v", v, r.path, r.sloc, r.offB, r.onB, r.offPct, r.onPct)
			}
		}
		t.Logf("changed blocks (%d):", len(changes[v]))
		for _, c := range changes[v] {
			t.Log(c)
		}
	}
}

// dupCheckSplit fails t when split, variant S counted over the recorder,
// differs from what duplication reports for m with
// duplication.split_literal_runs on, so the shipped option is the rule
// that was measured.
func dupCheckSplit(t *testing.T, l *load.Module, m dupMeasurePkg, opts Options, split duptok.Result) {
	t.Helper()
	opts.SplitLiteralRuns = true
	got, err := Package(l, m.p, load.OSFiles{}, m.sloc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocks != split.Blocks || got.Pct != split.Pct || !slices.Equal(got.Locations, split.Locations) {
		t.Errorf("%s: variant S %d blocks %v%%, split_literal_runs %d blocks %v%%",
			m.p.PkgPath, split.Blocks, split.Pct, got.Blocks, got.Pct)
	}
}

// dupLiteralMask reports, per token of the block b's first occurrence,
// whether it is literal-only under the sign-aware rule.
func dupLiteralMask(s *duptoktest.Recorder, b duptoktest.Block) []bool {
	mask := make([]bool, b.N)
	for i, tk := range s.Tokens[b.Pos[0] : b.Pos[0]+b.N] {
		switch tk.Class {
		case duptok.Literal, duptok.Punct, duptok.Sign:
			mask[i] = true
		}
	}
	return mask
}

// dupShift returns b's occurrences moved k tokens right with length n.
func dupShift(b duptoktest.Block, k, n int32) duptoktest.Block {
	pos := make([]int32, len(b.Pos))
	for i, p := range b.Pos {
		pos[i] = p + k
	}
	return duptoktest.Block{Pos: pos, N: n, Lead: b.Lead + k}
}

// dupUniq drops blocks with the same first occurrence and length as an
// earlier one.
func dupUniq(blocks []duptoktest.Block) []duptoktest.Block {
	type key struct{ p, n int32 }
	seen := make(map[key]bool, len(blocks))
	return slices.DeleteFunc(blocks, func(b duptoktest.Block) bool {
		k := key{b.Pos[0], b.N}
		if seen[k] {
			return true
		}
		seen[k] = true
		return false
	})
}

// dupTrimRuns is variant T: trim literal-only prefixes and suffixes off
// every block, dropping it when fewer than minTokens tokens remain.
func dupTrimRuns(s *duptoktest.Recorder, blocks []duptoktest.Block, minTokens int) []duptoktest.Block {
	out := make([]duptoktest.Block, 0, len(blocks))
	for _, b := range blocks {
		mask := dupLiteralMask(s, b)
		lo, hi := int32(0), b.N
		for lo < hi && mask[lo] {
			lo++
		}
		for hi > lo && mask[hi-1] {
			hi--
		}
		if int(hi-lo) >= minTokens {
			out = append(out, dupShift(b, lo, hi-lo))
		}
	}
	return dupUniq(out)
}

// dupSplitRuns is variant S: split every block at each literal-only run of
// at least runTokens tokens and keep the parts of at least minTokens.
func dupSplitRuns(s *duptoktest.Recorder, blocks []duptoktest.Block, runTokens, minTokens int) []duptoktest.Block {
	out := make([]duptoktest.Block, 0, len(blocks))
	for _, b := range blocks {
		mask := dupLiteralMask(s, b)
		from := int32(0)
		emit := func(to int32) {
			if int(to-from) >= minTokens {
				out = append(out, dupShift(b, from, to-from))
			}
		}
		for i := int32(0); i < b.N; {
			if !mask[i] {
				i++
				continue
			}
			j := i
			for j < b.N && mask[j] {
				j++
			}
			if int(j-i) >= runTokens {
				emit(i)
				from = j
			}
			i = j
		}
		emit(b.N)
	}
	return dupUniq(out)
}

// dupOccDiff returns, prefixed by mark and pkg, the blocks of a whose first
// occurrence and length are not a block of b, with their first two
// occurrences, occurrence count and length.
func dupOccDiff(s *duptoktest.Recorder, pkg, mark string, a, b []duptoktest.Block) []string {
	var out []string
	for _, x := range a {
		if slices.ContainsFunc(b, func(y duptoktest.Block) bool { return y.Pos[0] == x.Pos[0] && y.N == x.N }) {
			continue
		}
		line := mark + " " + pkg
		for _, p := range x.Pos[:min(2, len(x.Pos))] {
			loc := s.Location(p, x.N)
			line += " " + filepath.Base(loc.File) + ":" + strconv.Itoa(loc.StartLine) + "-" + strconv.Itoa(loc.EndLine)
		}
		out = append(out, line+" x"+strconv.Itoa(len(x.Pos))+" n"+strconv.Itoa(int(x.N)))
	}
	return out
}
