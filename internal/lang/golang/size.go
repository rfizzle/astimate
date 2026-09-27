package golang

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"

	"golang.org/x/tools/go/packages"
)

// sizeCounts holds the size metrics of one package, computed from its
// non-test files, generated files included.
type sizeCounts struct {
	files           int
	sloc            int
	largestFileSLOC int
	exportedSymbols int
	// perFile maps the absolute filename of each non-test file to its SLOC,
	// for metrics that weigh lines per file, such as duplication coverage.
	perFile map[string]int
}

// size computes files, sloc, largest_file_sloc and exported_symbols for p
// from its non-test files. It reads each file once to count source lines.
func size(l *loaded, p *packages.Package) (sizeCounts, error) {
	c := sizeCounts{
		files:   len(p.GoFiles),
		perFile: make(map[string]int, len(p.Syntax)),
	}
	for _, f := range p.Syntax {
		tf := l.fset.File(f.FileStart)
		if tf == nil {
			return sizeCounts{}, fmt.Errorf("counting lines of %s: file not in file set", p.PkgPath)
		}
		src, err := os.ReadFile(tf.Name())
		if err != nil {
			return sizeCounts{}, fmt.Errorf("counting lines of %s: %w", p.PkgPath, err)
		}
		n := fileSLOC(tf, f, src)
		c.perFile[tf.Name()] = n
		c.sloc += n
		c.largestFileSLOC = max(c.largestFileSLOC, n)
		c.exportedSymbols += exportedSymbols(f)
	}
	return c, nil
}

// commentSpan is the byte range [start, end) of one comment in a file.
type commentSpan struct{ start, end int }

// fileSLOC counts the lines of src that hold at least one non-space byte
// outside every comment of f. A line with code and a trailing comment
// counts; blank lines and lines entirely inside comments do not. tf is the
// token.File f was parsed into, used to turn comment positions into byte
// offsets in src.
func fileSLOC(tf *token.File, f *ast.File, src []byte) int {
	spans := make([]commentSpan, 0, len(f.Comments))
	for _, g := range f.Comments {
		for _, c := range g.List {
			spans = append(spans, commentSpan{tf.Offset(c.Pos()), tf.Offset(c.End())})
		}
	}

	n, ci, code := 0, 0, false
	for i, b := range src {
		if b == '\n' {
			if code {
				n++
			}
			code = false
			continue
		}
		if code || isSpace(b) {
			continue
		}
		for ci < len(spans) && spans[ci].end <= i {
			ci++
		}
		if ci == len(spans) || i < spans[ci].start {
			code = true
		}
	}
	if code {
		n++
	}
	return n
}

// isSpace reports whether b is ASCII white space other than a newline.
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\v' || b == '\f'
}

// exportedSymbols counts the exported top-level funcs, types, var and const
// names, and exported methods on any receiver, exported or not, declared in
// f. Struct fields and interface methods are not counted.
func exportedSymbols(f *ast.File) int {
	n := 0
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Name.IsExported() {
				n++
			}
		case *ast.GenDecl:
			for _, s := range d.Specs {
				n += exportedInSpec(s)
			}
		}
	}
	return n
}

// exportedInSpec counts the exported names a type, var or const spec
// declares. Import specs declare none.
func exportedInSpec(s ast.Spec) int {
	n := 0
	switch s := s.(type) {
	case *ast.TypeSpec:
		if s.Name.IsExported() {
			n++
		}
	case *ast.ValueSpec:
		for _, name := range s.Names {
			if name.IsExported() {
				n++
			}
		}
	}
	return n
}
