package golang

import (
	"fmt"
	"go/ast"
	"go/parser"
	"slices"

	"golang.org/x/tools/go/packages"
)

// sourceSyntax returns the syntax trees of p's non-test source files, one per
// entry of p.GoFiles, for the metrics that measure what a person wrote:
// size, globals, complexity, imports and duplication. For most packages that
// is p.Syntax. For a cgo package p.Syntax holds the files cgo generated
// (the rewritten sources plus _cgo_gotypes.go and friends, stored in the
// build cache), so loadModule parses p.GoFiles once more and sourceSyntax
// returns those trees instead. p.Syntax and p.TypesInfo stay the view for
// type-dependent metrics, whose object identities live in the generated
// trees. A nil l, as in unit tests on hand-built packages, means p.Syntax.
func sourceSyntax(l *loaded, p *packages.Package) []*ast.File {
	if l != nil {
		if files, ok := l.sources[p.PkgPath]; ok {
			return files
		}
	}
	return p.Syntax
}

// authoredSyntax returns the trees of sourceSyntax(l, p) that ast.IsGenerated
// does not report: the files a person wrote, which the size and structure
// metrics count (SPEC.md 6.5). A generated file, one with a
// "// Code generated ... DO NOT EDIT." line before its package clause, is
// regenerated in a rebuild, not written, so it adds nothing to sloc,
// largest_file_sloc, tokens_est, func_count, the cognitive metrics,
// max_nesting, globals, init_funcs, exported_symbols, untested_exports or
// duplication. files, generated_files and the import metrics still read
// every tree through sourceSyntax, since generated code imports real
// packages. When no file is generated the result is sourceSyntax's slice
// itself, not a copy, so callers must not modify it.
func authoredSyntax(l *loaded, p *packages.Package) []*ast.File {
	files := sourceSyntax(l, p)
	i := slices.IndexFunc(files, ast.IsGenerated)
	if i < 0 {
		return files
	}
	out := make([]*ast.File, i, len(files)-1)
	copy(out, files[:i])
	for _, f := range files[i+1:] {
		if !ast.IsGenerated(f) {
			out = append(out, f)
		}
	}
	return out
}

// generatedNames returns the absolute names of p's non-test source files
// that ast.IsGenerated reports, the files authoredSyntax leaves out, or nil
// when there are none. The names are those of p.GoFiles: sourceSyntax
// parses each entry of p.GoFiles into l.fset under its own name. A nil l
// resolves the trees in p.Fset.
func generatedNames(l *loaded, p *packages.Package) map[string]bool {
	fset := p.Fset
	if l != nil && l.fset != nil {
		fset = l.fset
	}
	var names map[string]bool
	for _, f := range sourceSyntax(l, p) {
		if !ast.IsGenerated(f) || fset == nil {
			continue
		}
		if tf := fset.File(f.FileStart); tf != nil {
			if names == nil {
				names = make(map[string]bool)
			}
			names[tf.Name()] = true
		}
	}
	return names
}

// parseSources parses the source files of every non-test module package
// whose syntax trees are not its source files, a cgo package, into l.fset in
// the mode go/packages parses with (comments and object resolution, which
// gocognit uses to spot recursion), and records them in l.sources. It runs
// once per load, before l is shared, so l.sources is read-only afterwards.
func parseSources(l *loaded) error {
	for _, path := range l.paths {
		p := l.pkgs[path]
		if syntaxIsSource(l, p) {
			continue
		}
		files := make([]*ast.File, 0, len(p.GoFiles))
		for _, name := range p.GoFiles {
			f, err := parser.ParseFile(l.fset, name, nil, parser.AllErrors|parser.ParseComments)
			if err != nil {
				return fmt.Errorf("parsing sources of %s: %w", path, err)
			}
			files = append(files, f)
		}
		if l.sources == nil {
			l.sources = make(map[string][]*ast.File)
		}
		l.sources[path] = files
	}
	return nil
}

// syntaxIsSource reports whether p.Syntax is exactly one tree per file of
// p.GoFiles, in l.fset under the same names. It is false for a cgo package,
// whose trees are the generated files go list compiles instead.
func syntaxIsSource(l *loaded, p *packages.Package) bool {
	if len(p.Syntax) != len(p.GoFiles) {
		return false
	}
	names := make(map[string]bool, len(p.GoFiles))
	for _, name := range p.GoFiles {
		names[name] = true
	}
	for _, f := range p.Syntax {
		tf := l.fset.File(f.FileStart)
		if tf == nil || !names[tf.Name()] {
			return false
		}
	}
	return true
}
