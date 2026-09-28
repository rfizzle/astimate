package inspect

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strings"

	sitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/rfizzle/astimate/internal/lang/typescript/internal/walk"
)

// Facts is everything the metrics need from one file, gathered in a single
// walk of its syntax tree. The tree and the file's bytes are not kept.
type Facts struct {
	// Abs is the absolute path; Dir the slash directory of the file
	// relative to the module root, which relative imports resolve against.
	Abs, Dir string
	Test     bool
	// Size is the file's length in bytes; O200k its o200k_base token count
	// when the scanner counts them.
	Size, O200k int
	// SLOC counts the lines holding a byte outside comments and white
	// space; CodeLines marks them by 1-based line.
	SLOC      int
	CodeLines []bool

	// Imports lists every module specifier the file imports or re-exports
	// from, in source order, duplicates included.
	Imports []string
	// Reexports records each export ... from statement with the number of
	// names it exports.
	Reexports []Reexport
	// Exports counts exported_symbols declared in this file, re-exports
	// excluded.
	Exports int
	// ExportedTypes counts exported classes, interfaces, type aliases and
	// enums; ExportedInterfaces the interfaces among them.
	ExportedTypes, ExportedInterfaces int
	// ExportedFuncs lists the exported functions and public methods of
	// exported classes, the candidates for untested_exports.
	ExportedFuncs []ExportedFunc
	// Globals holds the 1-based line of each name top-level let and var
	// declarations bind, in source order; its length is the file's count.
	Globals []int
	// GlobalNames holds the name of each of Globals, index for index.
	GlobalNames []string
	// HasInit reports a top-level statement that is a call.
	HasInit bool
	// Funcs holds the complexity of each function, in source order.
	Funcs []walk.Func

	// TestFuncs counts the test cases of a test file.
	TestFuncs int
	// Idents holds the text of every identifier of a test file.
	Idents map[string]bool

	// Tokens is the file's normalized token stream for duplication.
	Tokens walk.Tokens
}

// Reexport is one export ... from statement: its source specifier and the
// number of names it adds to the package's exports.
type Reexport struct {
	Spec  string
	Names int
}

// ExportedFunc is a candidate for untested_exports: the name tests must
// mention and the name reported for it. Directed reports the untested
// directive, which leaves it out of the count; Line is the 1-based line of
// its declaration.
type ExportedFunc struct {
	Match, Display string
	Directed       bool
	Line           int
}

// Options carries the extractor settings a scan needs.
type Options struct {
	// O200k makes the scanner count each file's o200k_base tokens.
	O200k bool
	// Logger receives one info record per file with syntax errors; nil
	// discards them.
	Logger *slog.Logger
}

// Scanner parses files and extracts their facts. It holds one parser per
// grammar and the module's token interning table, so one Scanner serves
// one module load; it is not safe for concurrent use.
type Scanner struct {
	opts    Options
	parsers map[*sitter.Language]*sitter.Parser
	kinds   *walk.Kinds
	counter *o200kCounter
}

// NewScanner returns a scanner for one module load.
func NewScanner(opts Options) *Scanner {
	return &Scanner{
		opts:    opts,
		parsers: make(map[*sitter.Language]*sitter.Parser, 2),
		kinds:   walk.NewKinds(),
	}
}

// Scan reads and parses the file at the absolute path abs, whose slash
// path relative to the module root is rel, a test file when test is set,
// and returns its facts. The file is read once and its bytes serve every
// metric; neither they nor the tree outlive the call.
func (s *Scanner) Scan(abs, rel string, test bool) (*Facts, error) {
	lang := languageFor(rel)
	src, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", abs, err)
	}
	p, ok := s.parsers[lang]
	if !ok {
		p = sitter.NewParser(lang)
		s.parsers[lang] = p
	}
	tree, err := p.ParseStrict(src)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", abs, err)
	}
	defer tree.Release()
	root := tree.RootNode()
	if root.HasError() && s.opts.Logger != nil {
		s.opts.Logger.Info("syntax errors", "file", abs)
	}

	ff := &Facts{Abs: abs, Dir: path.Dir(rel), Test: test, Size: len(src)}
	if s.opts.O200k {
		if s.counter == nil {
			if s.counter, err = newO200kCounter(); err != nil {
				return nil, err
			}
		}
		ff.O200k = s.counter.count(src)
	}
	w := &walker{Walker: *walk.New(lang, src, test, s.kinds), lang: lang, f: ff}
	w.program(root)
	ff.Imports, ff.Funcs, ff.Idents, ff.Tokens = w.Imports, w.Funcs, w.Idents, w.Tokens
	ff.SLOC, ff.CodeLines = countSLOC(src, w.Comments)
	return ff, nil
}

// languageFor returns the grammar for the file at rel: TSX for .tsx files,
// TypeScript otherwise, including .mts and .cts.
func languageFor(rel string) *sitter.Language {
	if strings.HasSuffix(rel, ".tsx") {
		return grammars.TsxLanguage()
	}
	return grammars.TypescriptLanguage()
}

// countSLOC counts the lines of src that hold at least one non-space byte
// outside every comment in comments, which are in source order, and marks
// them by 1-based line.
func countSLOC(src []byte, comments []walk.Span) (int, []bool) {
	lines := make([]bool, bytes.Count(src, []byte{'\n'})+2)
	n, ci, line, code := 0, 0, 1, false
	for i, b := range src {
		if b == '\n' {
			if code {
				n++
				lines[line] = true
			}
			code = false
			line++
			continue
		}
		if code || isSpace(b) {
			continue
		}
		for ci < len(comments) && comments[ci].End <= i {
			ci++
		}
		if ci == len(comments) || i < comments[ci].Start {
			code = true
		}
	}
	if code {
		n++
		lines[line] = true
	}
	return n, lines
}

// isSpace reports whether b is ASCII white space other than a newline.
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\v' || b == '\f'
}

// isTestCallee reports whether name is a test-case function: it, test or
// bench.
func isTestCallee(name string) bool {
	return name == "it" || name == "test" || name == "bench"
}

// isStringNode reports whether a node of type typ is a string or template
// literal.
func isStringNode(typ string) bool {
	return typ == "string" || typ == "template_string"
}
