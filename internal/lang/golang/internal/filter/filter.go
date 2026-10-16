// Package filter drops the files the configuration's exclude list matches
// (package exclude) from a loaded Go package, for the Go extractor's load
// (SPEC.md 6.5). A package keeps its type information; its file lists and
// syntax trees, which every metric reads, lose the excluded files.
package filter

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/exclude"
	"golang.org/x/tools/go/packages"
)

// File is one excluded file.
type File struct {
	// Rel is the file's slash path relative to the module root.
	Rel string
	// Pattern is the first pattern that matched it.
	Pattern string
}

// Filter applies one exclude list to the packages of one module load. It
// records each file it drops once (Files). It is not safe for concurrent
// use.
type Filter struct {
	// roots are the absolute module root and, when it differs, its
	// symlink-resolved form, since go list may name files either way.
	roots []string
	list  *exclude.List
	fset  *token.FileSet
	files []File
	seen  map[string]bool
}

// New returns a filter of list for the module at the absolute path root,
// whose syntax trees are in fset.
func New(root string, list *exclude.List, fset *token.FileSet) *Filter {
	f := &Filter{roots: []string{root}, list: list, fset: fset, seen: make(map[string]bool)}
	if resolved, err := filepath.EvalSymlinks(root); err == nil && resolved != root {
		f.roots = append(f.roots, resolved)
	}
	return f
}

// Files returns the files dropped so far, sorted by path.
func (f *Filter) Files() []File {
	out := slices.Clone(f.files)
	slices.SortFunc(out, func(a, b File) int { return strings.Compare(a.Rel, b.Rel) })
	return out
}

// IsTest reports whether the file at name is a _test.go file.
func IsTest(name string) bool { return strings.HasSuffix(name, "_test.go") }

// Package returns p with its excluded files dropped from GoFiles,
// CompiledGoFiles and Syntax, or p itself when none is. When p had
// non-test files and every one is excluded, gone is the first pattern, in
// list order, that matched one of them; it is "" otherwise.
func (f *Filter) Package(p *packages.Package) (out *packages.Package, gone string) {
	goFiles, first := f.names(p.GoFiles)
	compiled, _ := f.names(p.CompiledGoFiles)
	syntax := f.Trees(p.Syntax, true)
	if len(goFiles) == len(p.GoFiles) && len(compiled) == len(p.CompiledGoFiles) && len(syntax) == len(p.Syntax) {
		return p, ""
	}
	cp := *p
	cp.GoFiles, cp.CompiledGoFiles, cp.Syntax = goFiles, compiled, syntax
	if first != "" && !slices.ContainsFunc(goFiles, func(n string) bool { return !IsTest(n) }) {
		gone = first
	}
	return &cp, gone
}

// names returns the names not excluded, and the first pattern, in list
// order, that excluded one of the non-test names.
func (f *Filter) names(names []string) (kept []string, first string) {
	kept = make([]string, 0, len(names))
	firstIdx := -1
	for _, name := range names {
		i, pattern := f.match(name)
		switch {
		case i < 0:
			kept = append(kept, name)
		case !IsTest(name) && (firstIdx < 0 || i < firstIdx):
			firstIdx, first = i, pattern
		}
	}
	return kept, first
}

// Trees returns the trees of files not excluded. With byPosition a tree is
// named by the position of its package clause, which a //line directive
// maps back to the source file for the trees cgo generates; otherwise by
// the file it was parsed from.
func (f *Filter) Trees(files []*ast.File, byPosition bool) []*ast.File {
	kept := make([]*ast.File, 0, len(files))
	for _, file := range files {
		name := ""
		if byPosition {
			name = f.fset.Position(file.Package).Filename
		} else if tf := f.fset.File(file.FileStart); tf != nil {
			name = tf.Name()
		}
		if i, _ := f.match(name); i >= 0 {
			continue
		}
		kept = append(kept, file)
	}
	return kept
}

// match returns the index and text of the pattern that excludes the file
// at the absolute name, recording the file the first time, or -1 when
// none does or the file is outside the module root.
func (f *Filter) match(name string) (int, string) {
	rel := f.rel(name)
	if rel == "" {
		return -1, ""
	}
	i, pattern, ok := f.list.Match(rel)
	if !ok {
		return -1, ""
	}
	if !f.seen[rel] {
		f.seen[rel] = true
		f.files = append(f.files, File{Rel: rel, Pattern: pattern})
	}
	return i, pattern
}

// rel returns the slash path of the absolute name relative to the module
// root, or "" when it is not absolute or lies outside.
func (f *Filter) rel(name string) string {
	if !filepath.IsAbs(name) {
		return ""
	}
	for _, root := range f.roots {
		if r, err := filepath.Rel(root, name); err == nil {
			r = filepath.ToSlash(r)
			if r != ".." && !strings.HasPrefix(r, "../") {
				return r
			}
		}
	}
	return ""
}
