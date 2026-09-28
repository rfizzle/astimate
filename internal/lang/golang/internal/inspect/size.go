package inspect

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// SizeCounts holds the size metrics of one package. Files counts every
// non-test file; the rest are computed from the non-test files a person
// wrote, generated files excluded (see load.Module.AuthoredSyntax).
type SizeCounts struct {
	Files           int
	SLOC            int
	LargestFileSLOC int
	// LargestFile is the absolute filename of the first file with
	// LargestFileSLOC lines; empty when the package has no authored files.
	LargestFile string
	// Exports holds exported_symbols and the exported type counts that
	// abstractness is computed from.
	Exports ExportCounts
	// PerFile maps the absolute filename of each authored non-test file to
	// its SLOC, for metrics that weigh lines per file, such as duplication
	// coverage.
	PerFile map[string]int
}

// Size computes files over every non-test file of p, and sloc,
// largest_file_sloc and exported_symbols, with the exported type counts
// behind abstractness, over the authored ones (load.Module.AuthoredSyntax),
// read through src to count source lines. A generated file is counted in
// files only. It iterates the trees of p.GoFiles, so for a cgo package too
// PerFile has one entry per counted file. Interface types are recognized
// through p's package scope when p has type information (see
// exportedInSpec).
func Size(m *load.Module, p *packages.Package, src load.FileSource) (SizeCounts, error) {
	c := SizeCounts{
		Files:   len(p.GoFiles),
		PerFile: make(map[string]int, len(p.GoFiles)),
	}
	var scope *types.Scope
	if p.Types != nil {
		scope = p.Types.Scope()
	}
	for _, f := range m.AuthoredSyntax(p) {
		tf := m.Fset.File(f.FileStart)
		if tf == nil {
			return SizeCounts{}, fmt.Errorf("counting lines of %s: file not in file set", p.PkgPath)
		}
		data, err := src.Read(tf.Name())
		if err != nil {
			return SizeCounts{}, fmt.Errorf("counting lines of %s: %w", p.PkgPath, err)
		}
		n := FileSLOC(tf, f, data)
		c.PerFile[tf.Name()] = n
		c.SLOC += n
		if c.LargestFile == "" || n > c.LargestFileSLOC {
			c.LargestFile, c.LargestFileSLOC = tf.Name(), n
		}
		c.Exports.add(ExportedSymbols(f, scope))
	}
	return c, nil
}

// commentSpan is the byte range [start, end) of one comment in a file.
type commentSpan struct{ start, end int }

// FileSLOC counts the lines of src that hold at least one non-space byte
// outside every comment of f. A line with code and a trailing comment
// counts; blank lines and lines entirely inside comments do not. tf is the
// token.File f was parsed into, used to turn comment positions into byte
// offsets in src.
func FileSLOC(tf *token.File, f *ast.File, src []byte) int {
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
		if code || IsSpace(b) {
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

// IsSpace reports whether b is ASCII white space other than a newline, the
// bytes FileSLOC does not count as code.
func IsSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\v' || b == '\f'
}

// ExportCounts holds the exported declarations of one file or package.
type ExportCounts struct {
	// Symbols is exported_symbols: funcs, methods, types, vars and consts.
	Symbols int
	// Types counts exported type specs, aliases included.
	Types int
	// InterfaceTypes counts the exported type specs whose type is an
	// interface; see exportedInSpec.
	InterfaceTypes int
}

// add accumulates o into c.
func (c *ExportCounts) add(o ExportCounts) {
	c.Symbols += o.Symbols
	c.Types += o.Types
	c.InterfaceTypes += o.InterfaceTypes
}

// ExportedSymbols counts the exported top-level funcs, types, var and const
// names, and exported methods on any receiver, exported or not, declared in
// f, with the exported types among them and the interface types among those.
// Struct fields and interface methods are not counted. scope is the package
// scope interface types are resolved in, or nil to test syntactically; see
// exportedInSpec.
func ExportedSymbols(f *ast.File, scope *types.Scope) ExportCounts {
	var c ExportCounts
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Name.IsExported() {
				c.Symbols++
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
func exportedInSpec(s ast.Spec, scope *types.Scope) ExportCounts {
	var c ExportCounts
	switch s := s.(type) {
	case *ast.TypeSpec:
		if s.Name.IsExported() {
			c.Symbols++
			c.Types++
			if isInterfaceSpec(s, scope) {
				c.InterfaceTypes++
			}
		}
	case *ast.ValueSpec:
		for _, name := range s.Names {
			if name.IsExported() {
				c.Symbols++
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
