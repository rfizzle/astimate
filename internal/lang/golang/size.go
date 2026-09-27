package golang

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"
)

// sizeCounts holds the size metrics of one package. files counts every
// non-test file; the rest are computed from the non-test files a person
// wrote, generated files excluded (see authoredSyntax).
type sizeCounts struct {
	files           int
	sloc            int
	largestFileSLOC int
	// largestFile is the absolute filename of the first file with
	// largestFileSLOC lines; empty when the package has no authored files.
	largestFile string
	// exports holds exported_symbols and the exported type counts that
	// abstractness is computed from.
	exports exportCounts
	// perFile maps the absolute filename of each authored non-test file to
	// its SLOC, for metrics that weigh lines per file, such as duplication
	// coverage.
	perFile map[string]int
}

// size computes files over every non-test file of p, and sloc,
// largest_file_sloc and exported_symbols, with the exported type counts
// behind abstractness, over the authored ones (authoredSyntax), read through
// src to count source lines. A generated file is counted in files only. It
// iterates the trees of p.GoFiles, so for a cgo package too perFile has one
// entry per counted file. Interface types are recognized through p's
// package scope when p has type information (see exportedInSpec).
func size(l *loaded, p *packages.Package, src fileSource) (sizeCounts, error) {
	c := sizeCounts{
		files:   len(p.GoFiles),
		perFile: make(map[string]int, len(p.GoFiles)),
	}
	var scope *types.Scope
	if p.Types != nil {
		scope = p.Types.Scope()
	}
	for _, f := range authoredSyntax(l, p) {
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
		if c.largestFile == "" || n > c.largestFileSLOC {
			c.largestFile, c.largestFileSLOC = tf.Name(), n
		}
		c.exports.add(exportedSymbols(f, scope))
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
	// interfaceTypes counts the exported type specs whose type is an
	// interface; see exportedInSpec.
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
// Struct fields and interface methods are not counted. scope is the package
// scope interface types are resolved in, or nil to test syntactically; see
// exportedInSpec.
func exportedSymbols(f *ast.File, scope *types.Scope) exportCounts {
	var c exportCounts
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Name.IsExported() {
				c.symbols++
			}
		case *ast.GenDecl:
			for _, s := range d.Specs {
				c.add(exportedInSpec(s, scope))
			}
		}
	}
	return c
}

// exportedInSpec counts the exported names a type, var or const spec
// declares. Import specs declare none.
//
// An exported type spec is an interface type when the type it declares has
// an interface as its underlying type, generic and constraint interfaces
// included, so a definition or alias naming an interface, such as
// type R io.Reader or type R = io.Reader, counts as well as an interface
// literal. The type is looked up by name in scope, the package scope, which
// holds the same declarations whether f is the tree go/packages
// type-checked or a cgo package's reparsed source. With a nil scope, or a
// name the scope lacks, the test falls back to syntax: the type expression
// is an interface literal.
func exportedInSpec(s ast.Spec, scope *types.Scope) exportCounts {
	var c exportCounts
	switch s := s.(type) {
	case *ast.TypeSpec:
		if s.Name.IsExported() {
			c.symbols++
			c.types++
			if isInterfaceSpec(s, scope) {
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

// isInterfaceSpec reports whether the type s declares is an interface: its
// underlying type in scope is one, or, when scope is nil or has no type of
// that name, its type expression is an interface literal.
func isInterfaceSpec(s *ast.TypeSpec, scope *types.Scope) bool {
	if scope != nil {
		if tn, ok := scope.Lookup(s.Name.Name).(*types.TypeName); ok {
			_, iface := tn.Type().Underlying().(*types.Interface)
			return iface
		}
	}
	_, ok := s.Type.(*ast.InterfaceType)
	return ok
}
