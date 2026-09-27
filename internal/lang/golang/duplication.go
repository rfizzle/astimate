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
// Each token goes into a duptok.Stream with its duptok class: a literal
// (LIT, or an interned literal text) is duptok.Literal; one of the
// punctuation tokens , { } : [ ] ( ) or an explicit ; is duptok.Punct; a
// unary sign (below) is duptok.Sign; everything else, identifiers included,
// is duptok.Code. Every file is closed with its name and its source lines,
// and duptok follows it with a separator code unique in the stream, so no
// repeat crosses a file boundary.
//
// Finder. The suffix-array finder, the merging rule, the literal-only rule
// (duplication.ignore_literal_only) and coverage are those of
// internal/lang/duptok, shared with the TypeScript extractor; its package
// comment gives the derivation. A literal-only block is one whose every
// token is Literal or Punct, so a repeated run of a literal table
// (precomputed points, lookup tables) is neither a block nor coverage, while
// any identifier, keyword or other operator keeps the block. The rule runs
// after merging, so a block that holds a literal table next to code is kept
// whole.
//
// Signed literals. The scanner marks as duptok.Sign each + or - that
// directly precedes an int, float, imaginary or char literal and follows a
// token that cannot end an operand (anything but an identifier, a literal,
// or ) ] }), which is to say a unary sign. With duplication.fold_signs on,
// the literal-only rule counts such a sign as part of its literal, so a
// table of negative numbers is dropped too. The stream keeps the sign as
// its own code: folding it into the literal there was measured and
// rejected, because it lets f(-1) match f(1), which merges signed
// coefficient tables with the code around them, and it shortens code blocks
// below duplication.min_tokens.
//
// Coverage. A token covers the lines from its first to its last (a
// multi-line raw string spans several), and a covered line counts only if
// it is a source line by the same rule size uses: at least one non-space
// byte outside comments. duplication_pct is covered lines over the package
// SLOC from size (which includes generated files), times 100, rounded to
// one decimal.

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/scanner"
	"go/token"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/rfizzle/astimate/internal/lang/duptok"
)

const (
	// dupIdentCode is the normalized code of every identifier (ID).
	dupIdentCode = int32(token.IDENT)
	// dupLitCode is the normalized code of every basic literal (LIT).
	dupLitCode = int32(token.INT)
	// dupInternBase is the first code given to an interned identifier or
	// literal text when its normalization is off. It is above every
	// token.Token value and far below duptok.SeparatorBase.
	dupInternBase = int32(1 << 12)
)

// dupOptions configures duplication. The extractor options WithDupMinTokens,
// WithDupIgnoreLiteralOnly and WithDupFoldSigns set minTokens,
// ignoreLiteralOnly and foldSigns; the normalization toggles keep their
// defaults.
type dupOptions struct {
	// minTokens is duplication.min_tokens: the shortest normalized token sequence
	// that counts as a duplicate block.
	minTokens int
	// normalizeIdents maps every identifier to one code.
	normalizeIdents bool
	// normalizeLiterals maps every string, char and numeric literal to one
	// code.
	normalizeLiterals bool
	// ignoreLiteralOnly is duplication.ignore_literal_only: drop a block made only
	// of literals and punctuation.
	ignoreLiteralOnly bool
	// foldSigns is duplication.fold_signs: under ignoreLiteralOnly, a unary + or -
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

// finder returns the options of o that the duptok finder takes.
func (o dupOptions) finder() duptok.Options {
	return duptok.Options{MinTokens: o.minTokens, IgnoreLiteralOnly: o.ignoreLiteralOnly, FoldSigns: o.foldSigns}
}

// tokenSink receives the normalized tokens of each file and then closes
// the file. *duptok.Stream implements it; tests record what scan emits.
type tokenSink interface {
	Add(code int32, class duptok.Class, line, last int) error
	EndFile(name string, code []bool)
}

// dupTokenizer turns Go source into duptok tokens under one set of options.
// It interns identifier or literal text when a normalization toggle is off,
// so the files of one stream go through one tokenizer.
type dupTokenizer struct {
	opts dupOptions
	// intern maps identifier or literal text to its code when a
	// normalization toggle is off.
	intern map[string]int32
}

// newDupTokenizer returns a tokenizer for opts.
func newDupTokenizer(opts dupOptions) *dupTokenizer {
	return &dupTokenizer{opts: opts, intern: make(map[string]int32)}
}

// duplication computes dup_blocks and duplication_pct for p from its
// non-test, non-generated files, read through src, with sz the size metrics
// of p (its sloc is the denominator of the percentage). See the file comment
// for the algorithm.
func duplication(l *loaded, p *packages.Package, src fileSource, sz sizeCounts, opts dupOptions) (duptok.Result, error) {
	if opts.minTokens < 1 {
		return duptok.Result{}, fmt.Errorf("detecting duplication in %s: minimum of %d tokens is not positive", p.PkgPath, opts.minTokens)
	}
	var s duptok.Stream
	if err := newDupTokenizer(opts).appendPackage(token.NewFileSet(), l, p, src, &s); err != nil {
		return duptok.Result{}, fmt.Errorf("detecting duplication in %s: %w", p.PkgPath, err)
	}
	res, err := s.Count(opts.finder(), sz.sloc)
	if err != nil {
		return duptok.Result{}, fmt.Errorf("detecting duplication in %s: %w", p.PkgPath, err)
	}
	return res, nil
}

// appendPackage scans the non-test, non-generated files of p, read through
// src and positioned in fs, into sink, closing each file.
func (z *dupTokenizer) appendPackage(fs *token.FileSet, l *loaded, p *packages.Package, src fileSource, sink tokenSink) error {
	for _, f := range sourceSyntax(l, p) {
		if ast.IsGenerated(f) {
			continue
		}
		tf := l.fset.File(f.FileStart)
		if tf == nil {
			return errors.New("file not in file set")
		}
		data, err := src.read(tf.Name())
		if err != nil {
			return err
		}
		if err := z.scan(fs, tf.Name(), data, sink); err != nil {
			return err
		}
	}
	return nil
}

// heldSign is a + or - that scan holds back until the next token shows
// whether it signs a numeric literal.
type heldSign struct {
	code       int32
	line, last int
	held       bool
}

// scan adds the normalized tokens of one file to sink under name, then
// closes the file. On a scan error the file is left open.
func (z *dupTokenizer) scan(fs *token.FileSet, name string, src []byte, sink tokenSink) error {
	tf := fs.AddFile(name, -1, len(src))
	var sc scanner.Scanner
	var scanErr error
	sc.Init(tf, src, func(pos token.Position, msg string) {
		if scanErr == nil {
			scanErr = fmt.Errorf("scanning %s: %s", pos, msg)
		}
	}, 0)
	// code reports, per 1-based line, whether the line holds a non-space
	// byte of some token, which is the SLOC rule size applies.
	code := make([]bool, bytes.Count(src, []byte{'\n'})+2)
	// operand: the previous token can end an operand; sign: the previous
	// token, not yet added, is a + or - that does not follow one.
	operand := false
	var sign heldSign
	for {
		pos, tok, lit := sc.Scan()
		if tok == token.EOF {
			break
		}
		if sign.held {
			class := duptok.Code
			if isNumericLit(tok) {
				class = duptok.Sign
			}
			if err := sink.Add(sign.code, class, sign.line, sign.last); err != nil {
				return err
			}
			sign.held = false
		}
		unary := !operand && (tok == token.SUB || tok == token.ADD)
		operand = endsOperand(tok)
		if tok == token.SEMICOLON && lit == "\n" {
			continue
		}
		line := tf.Line(pos)
		last := line
		code[line] = true
		if tok == token.STRING && strings.IndexByte(lit, '\n') >= 0 {
			last = markRawLines(code, line, lit)
		}
		c, class := z.code(tok, lit)
		if unary {
			sign = heldSign{code: c, line: line, last: last, held: true}
			continue
		}
		if err := sink.Add(c, class, line, last); err != nil {
			return err
		}
	}
	if sign.held {
		if err := sink.Add(sign.code, duptok.Code, sign.line, sign.last); err != nil {
			return err
		}
	}
	if scanErr != nil {
		return scanErr
	}
	sink.EndFile(name, code)
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
func markRawLines(code []bool, line int, lit string) int {
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

// code returns the stream code and duptok class of one token under the
// tokenizer's options.
func (z *dupTokenizer) code(tok token.Token, lit string) (int32, duptok.Class) {
	switch {
	case tok == token.IDENT:
		if z.opts.normalizeIdents {
			return dupIdentCode, duptok.Code
		}
		return z.interned("i" + lit), duptok.Code
	case tok.IsLiteral():
		if z.opts.normalizeLiterals {
			return dupLitCode, duptok.Literal
		}
		return z.interned("l" + lit), duptok.Literal
	}
	switch tok {
	case token.COMMA, token.LBRACE, token.RBRACE, token.COLON,
		token.LBRACK, token.RBRACK, token.LPAREN, token.RPAREN, token.SEMICOLON:
		return int32(tok), duptok.Punct
	}
	return int32(tok), duptok.Code
}

// interned returns the code of key, assigning the next free one on first
// use.
func (z *dupTokenizer) interned(key string) int32 {
	c, ok := z.intern[key]
	if !ok {
		c = dupInternBase + int32(len(z.intern))
		z.intern[key] = c
	}
	return c
}
