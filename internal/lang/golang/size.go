package golang

import (
	"fmt"
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/packages"
)

// sizeCounts holds the size metrics of one package, computed from its
// non-test files, generated files included.
type sizeCounts struct {
	files           int
	sloc            int
	largestFileSLOC int
	// exports holds exported_symbols and the exported type counts that
	// abstractness is computed from.
	exports exportCounts
	// perFile maps the absolute filename of each non-test file to its SLOC,
	// for metrics that weigh lines per file, such as duplication coverage.
	perFile map[string]int
}

// size computes files, sloc, largest_file_sloc and exported_symbols for p
// from its non-test files, with the exported type counts behind
// abstractness, read through src to count source lines. It iterates
// sourceSyntax, the trees of p.GoFiles, so for a cgo package too perFile has
// one entry per counted file.
func size(l *loaded, p *packages.Package, src fileSource) (sizeCounts, error) {
	c := sizeCounts{
		files:   len(p.GoFiles),
		perFile: make(map[string]int, len(p.GoFiles)),
	}
	for _, f := range sourceSyntax(l, p) {
		tf := l.fset.File(f.FileStart)
		if tf == nil {
			return sizeCounts{}, fmt.Errorf("counting lines of %s: file not in file set", p.PkgPath)
		}
		data, err := src.read(tf.Name())
		if err != nil {
			return sizeCounts{}, fmt.Errorf("counting lines of %s: %w", p.PkgPath, err)
		}
		n := fileSLOC(tf, f, data)
		c.perFile[tf.Name()] = n
		c.sloc += n
		c.largestFileSLOC = max(c.largestFileSLOC, n)
		c.exports.add(exportedSymbols(f))
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

// exportCounts holds the exported declarations of one file or package.
type exportCounts struct {
	// symbols is exported_symbols: funcs, methods, types, vars and consts.
	symbols int
	// types counts exported type specs, aliases included.
	types int
	// interfaceTypes counts the exported type specs whose type expression
	// is an interface literal.
	interfaceTypes int
}

// add accumulates o into c.
func (c *exportCounts) add(o exportCounts) {
	c.symbols += o.symbols
	c.types += o.types
	c.interfaceTypes += o.interfaceTypes
}

// exportedSymbols counts the exported top-level funcs, types, var and const
// names, and exported methods on any receiver, exported or not, declared in
// f, with the exported types among them and the interface types among those.
// Struct fields and interface methods are not counted.
func exportedSymbols(f *ast.File) exportCounts {
	var c exportCounts
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Name.IsExported() {
				c.symbols++
			}
		case *ast.GenDecl:
			for _, s := range d.Specs {
				c.add(exportedInSpec(s))
			}
		}
	}
	return c
}

// exportedInSpec counts the exported names a type, var or const spec
// declares. Import specs declare none.
//
// An exported type spec is an interface type when its type expression is an
// interface literal, generic and constraint interfaces included. The test is
// syntactic, so an alias of an interface literal (type I = interface{ M() })
// counts, while an alias or definition naming another type, such as
// type R = io.Reader or type R io.Reader, does not, even when that type is an
// interface: resolving it would need type information for one ratio's input.
func exportedInSpec(s ast.Spec) exportCounts {
	var c exportCounts
	switch s := s.(type) {
	case *ast.TypeSpec:
		if s.Name.IsExported() {
			c.symbols++
			c.types++
			if _, ok := s.Type.(*ast.InterfaceType); ok {
				c.interfaceTypes++
			}
		}
	case *ast.ValueSpec:
		for _, name := range s.Names {
			if name.IsExported() {
				c.symbols++
			}
		}
	}
	return c
}
