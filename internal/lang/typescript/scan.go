package typescript

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"strings"

	sitter "github.com/odvcencio/gotreesitter"
	"github.com/rfizzle/astimate/internal/lang/duptok"
)

// fileFacts is everything the metrics need from one file, gathered in a
// single walk of its syntax tree. The tree and the file's bytes are not
// kept.
type fileFacts struct {
	// abs is the absolute path; dir the slash directory of the file
	// relative to the module root, which relative imports resolve against.
	abs, dir string
	test     bool
	// size is the file's length in bytes; o200k its o200k_base token count
	// when the load counts them.
	size, o200k int
	// sloc counts the lines holding a byte outside comments and white
	// space; codeLines marks them by 1-based line.
	sloc      int
	codeLines []bool

	// imports lists every module specifier the file imports or re-exports
	// from, in source order, duplicates included.
	imports []string
	// reexports records each export ... from statement with the number of
	// names it exports.
	reexports []reexport
	// exports counts exported_symbols declared in this file, re-exports
	// excluded.
	exports int
	// exportedTypes counts exported classes, interfaces, type aliases and
	// enums; exportedInterfaces the interfaces among them.
	exportedTypes, exportedInterfaces int
	// exportedFuncs lists the exported functions and public methods of
	// exported classes, the candidates for untested_exports.
	exportedFuncs []exportedFunc
	// globals counts the names top-level let and var declarations bind.
	globals int
	// hasInit reports a top-level statement that is a call.
	hasInit bool
	// funcs holds the complexity of each function, in source order.
	funcs []funcScore

	// testFuncs counts the test cases of a test file.
	testFuncs int
	// idents holds the text of every identifier of a test file.
	idents map[string]bool

	// toks is the file's normalized token stream for duplication.
	toks tokenStream
}

// reexport is one export ... from statement: its source specifier and the
// number of names it adds to the package's exports.
type reexport struct {
	spec  string
	names int
}

// exportedFunc is a candidate for untested_exports: the name tests must
// mention and the name reported for it. directed reports the untested
// directive, which leaves it out of the count.
type exportedFunc struct {
	match, display string
	directed       bool
}

// tokenStream is the normalized tokens of one file, parallel slices.
type tokenStream struct {
	codes      []int32
	class      []duptok.Class
	line, last []int32
}

// Normalized token codes. Every other token kind is interned from
// firstInterned upwards, per module.
const (
	identCode     = int32(0)
	literalCode   = int32(1)
	firstInterned = int32(2)
)

// scanner parses files and extracts their facts. It holds one parser per
// grammar and the module's token interning table; it is not safe for
// concurrent use.
type scanner struct {
	opts    loadOptions
	parsers map[*sitter.Language]*sitter.Parser
	intern  map[string]int32
	counter *o200kCounter
}

// newScanner returns a scanner for one module load.
func newScanner(opts loadOptions) *scanner {
	return &scanner{
		opts:    opts,
		parsers: make(map[*sitter.Language]*sitter.Parser, 2),
		intern:  make(map[string]int32),
	}
}

// scanFile reads and parses f and returns its facts.
func (s *scanner) scanFile(f sourceFile) (*fileFacts, error) {
	src, err := os.ReadFile(f.abs)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", f.abs, err)
	}
	lang := languageFor(f.rel)
	p, ok := s.parsers[lang]
	if !ok {
		p = sitter.NewParser(lang)
		s.parsers[lang] = p
	}
	tree, err := p.ParseStrict(src)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", f.abs, err)
	}
	defer tree.Release()
	root := tree.RootNode()
	if root.HasError() && s.opts.logger != nil {
		s.opts.logger.Info("syntax errors", "file", f.abs)
	}

	ff := &fileFacts{abs: f.abs, dir: path.Dir(f.rel), test: f.test, size: len(src)}
	if f.test {
		ff.idents = make(map[string]bool)
	}
	if s.opts.o200k {
		if s.counter == nil {
			if s.counter, err = newO200kCounter(); err != nil {
				return nil, err
			}
		}
		ff.o200k = s.counter.count(src)
	}
	w := &walker{s: s, lang: lang, src: src, f: ff}
	w.program(root)
	if err := w.err; err != nil {
		return nil, fmt.Errorf("scanning %s: %w", f.abs, err)
	}
	ff.sloc, ff.codeLines = countSLOC(src, w.comments)
	return ff, nil
}

// code returns the interned code of a token kind.
func (s *scanner) code(kind string) int32 {
	c, ok := s.intern[kind]
	if !ok {
		c = firstInterned + int32(len(s.intern))
		s.intern[kind] = c
	}
	return c
}

// span is the byte range [start, end) of one comment.
type span struct{ start, end int }

// countSLOC counts the lines of src that hold at least one non-space byte
// outside every comment in comments, which are in source order, and marks
// them by 1-based line.
func countSLOC(src []byte, comments []span) (int, []bool) {
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
		for ci < len(comments) && comments[ci].end <= i {
			ci++
		}
		if ci == len(comments) || i < comments[ci].start {
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

// trimQuotes returns the contents of a string literal's source text.
func trimQuotes(s string) string {
	if len(s) >= 2 {
		return s[1 : len(s)-1]
	}
	return s
}

// isIdentType reports whether a named leaf of type typ is an identifier.
func isIdentType(typ string) bool {
	return strings.HasSuffix(typ, "identifier")
}
