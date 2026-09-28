// Package stub implements the signatures stub strategy of SPEC.md 11.2: it
// replaces every function and method body of a package's non-test files
// but init's with panic("not implemented"), and removes the package
// initialization code that would call the package's own code (variable
// initializers and init statements), keeping everything else byte for
// byte.
package stub

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/imports"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
)

// NoFilesError is the error Package returns for a directory without
// non-test Go files; Tree skips such a directory, a package of tests only.
type NoFilesError struct {
	// Dir is the directory.
	Dir string
}

// Error names the directory.
func (e *NoFilesError) Error() string {
	return "stubbing " + e.Dir + ": no non-test Go files"
}

// Panic is the message every stubbed body panics with.
const Panic = "not implemented"

// body replaces every function and method body but init's.
const body = "{\n\tpanic(\"" + Panic + "\")\n}"

// File is one stubbed source file.
type File struct {
	// Name is the file's path relative to the stubbed directory, in slash
	// form: its base name for a package.
	Name string
	// Data is the stubbed source.
	Data []byte
	// Summary counts what the stub did to the file beyond the panics; it
	// is not part of the hash.
	Summary
}

// Summary counts what a stub did beyond replacing bodies with the panic.
type Summary struct {
	// Inits is the number of init statements removed.
	Inits int
	// Vars is the number of package-level variable specs whose
	// initializers were removed.
	Vars int
}

// Summarize sums the counts of files.
func Summarize(files []File) Summary {
	var s Summary
	for _, f := range files {
		s.Inits += f.Inits
		s.Vars += f.Vars
	}
	return s
}

// Kinds of edit beyond a body.
const (
	editImport = iota // adds imports
	editVar           // removes a variable spec's initializer
	editInit          // removes an init statement
)

// edit replaces src[start:end] of a file with text.
type edit struct {
	start, end int
	text       string
	kind       int
}

// srcFile is one parsed source file of the package being stubbed.
type srcFile struct {
	name, path string
	src        []byte
	fset       *token.FileSet
	file       *ast.File
}

// Package returns the stubbed form of every non-test .go file in dir,
// sorted by name, under the signatures strategy: each function or method
// body but init's becomes panic("not implemented"), comments inside the
// bodies go with them, and the package initialization code that calls a
// function or method of the package loses it (see zeroEdits): a
// package-level variable's initializer, or a statement of an init body.
// Everything else is kept byte for byte, and the result is gofmt-formatted
// with the imports no file uses any more removed. Files of every build
// configuration are stubbed, and their //go:build lines are kept.
// Declarations without a body (assembly or linkname) are kept as they are.
// env is the environment of the go command that type-checks the package,
// which happens only when initialization code may call the package. The
// output depends only on the files' contents, the toolchain and
// golang.org/x/tools.
func Package(ctx context.Context, dir string, env []string) ([]File, error) {
	srcs, err := parseDir(dir)
	if err != nil {
		return nil, err
	}
	zero, err := zeroEdits(ctx, dir, env, srcs)
	if err != nil {
		return nil, err
	}
	out := make([]File, 0, len(srcs))
	for _, s := range srcs {
		f, err := s.stub(zero[s.name])
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// parseDir reads and parses every non-test .go file in dir, sorted by
// name.
func parseDir(dir string) ([]*srcFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("stubbing %s: %w", dir, err)
	}
	fset := token.NewFileSet()
	var out []*srcFile
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("stubbing %s: %w", path, err)
		}
		s, err := parseSrc(fset, path, src)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, &NoFilesError{Dir: dir}
	}
	slices.SortFunc(out, func(a, b *srcFile) int { return strings.Compare(a.name, b.name) })
	return out, nil
}

// source stubs one file's source with no initializer edits; path is used
// for error messages and lets imports.Process read the package's sibling
// files.
func source(path string, src []byte) ([]byte, error) {
	s, err := parseSrc(token.NewFileSet(), path, src)
	if err != nil {
		return nil, err
	}
	f, err := s.stub(nil)
	return f.Data, err
}

// parseSrc parses the source src of the file at path into fset.
func parseSrc(fset *token.FileSet, path string, src []byte) (*srcFile, error) {
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("stubbing %s: %w", path, err)
	}
	return &srcFile{name: filepath.Base(path), path: path, src: src, fset: fset, file: f}, nil
}

// stub applies the body edits and the extra edits (each removing an
// initializer, or adding an import when it removes nothing) to the file
// and formats the result, dropping the imports nothing refers to any more.
func (s *srcFile) stub(extra []edit) (File, error) {
	out := File{Name: s.name}
	edits := slices.Clone(extra)
	for _, e := range extra {
		out.Vars += b2i(e.kind == editVar)
		out.Inits += b2i(e.kind == editInit)
	}
	for _, decl := range s.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || isInit(fn) {
			continue
		}
		edits = append(edits, edit{start: s.offset(fn.Body.Lbrace), end: s.offset(fn.Body.Rbrace) + 1, text: body})
	}
	slices.SortFunc(edits, func(a, b edit) int { return cmp.Compare(a.start, b.start) })
	var b strings.Builder
	b.Grow(len(s.src))
	last := 0
	for _, e := range edits {
		b.Write(s.src[last:e.start])
		b.WriteString(e.text)
		last = e.end
	}
	b.Write(s.src[last:])
	// Stubbing deletes code, and the imports an initializer's type needs
	// are added explicitly, so imports.Process only removes imports; it
	// adds one only for an unresolved name, which a compiling package does
	// not have. TestSource pins that no import is added.
	data, err := imports.Process(s.path, []byte(b.String()), &imports.Options{
		Comments:  true,
		TabIndent: true,
		TabWidth:  8,
	})
	if err != nil {
		return File{}, fmt.Errorf("stubbing %s: formatting: %w", s.path, err)
	}
	out.Data = data
	return out, nil
}

// isInit reports whether fn is an init function, whose body runs before
// any test and so is kept but for the statements zeroEdits removes.
func isInit(fn *ast.FuncDecl) bool {
	return fn.Recv == nil && fn.Name.Name == "init"
}

// b2i is 1 for true and 0 for false.
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// offset returns the byte offset of pos in the file.
func (s *srcFile) offset(pos token.Pos) int {
	return s.fset.PositionFor(pos, false).Offset
}

// Write writes files into dir, replacing the originals.
func Write(dir string, files []File) error {
	for _, f := range files {
		path := filepath.Join(dir, filepath.FromSlash(f.Name))
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("writing stub: %w", err)
		}
		if err := os.WriteFile(path, f.Data, info.Mode().Perm()); err != nil {
			return fmt.Errorf("writing stub: %w", err)
		}
	}
	return nil
}

// TreeHash returns the hex SHA-256 of the stubbed files in the order given
// (Package and Tree sort them by name): each file contributes its name, a
// newline, its length in decimal, a newline and its bytes.
func TreeHash(files []File) string {
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Name + "\n" + strconv.Itoa(len(f.Data)) + "\n"))
		h.Write(f.Data)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Apply stubs the package of e, or every member of e's tree, in the clone
// at root and checks the result against e.StubSHA256, so a runner starts
// from the tree the selection verified. env is the go command's
// environment. It returns what the stub did beyond the panics.
func Apply(ctx context.Context, root string, e *definition.Experiment, env []string) (Summary, error) {
	dir := filepath.Join(root, filepath.FromSlash(e.Dir))
	var files []File
	var err error
	if len(e.Members) > 0 {
		dirs := make([]string, len(e.Members))
		for i, m := range e.Members {
			dirs[i] = m.Dir
		}
		files, err = Tree(ctx, root, e.Dir, dirs, env)
	} else {
		files, err = Package(ctx, dir, env)
	}
	if err != nil {
		return Summary{}, err
	}
	if got := TreeHash(files); got != e.StubSHA256 {
		return Summary{}, fmt.Errorf("stubbing %s: tree hash %s, want %s (different toolchain or tree?)", e.Package, got, e.StubSHA256)
	}
	return Summarize(files), Write(dir, files)
}

// Tree returns the stubbed files of every package directory in dirs, each
// module-relative and at or below the tree directory dir, in the module at
// root. Each file's Name is its path relative to dir, in slash form, and
// the files are sorted by Name. All packages are stubbed from the original
// tree before any is written.
func Tree(ctx context.Context, root, dir string, dirs []string, env []string) ([]File, error) {
	var out []File
	for _, d := range dirs {
		if !definition.InTree(dir, d) {
			return nil, fmt.Errorf("stubbing tree %s: %s is not below it", dir, d)
		}
		files, err := Package(ctx, filepath.Join(root, filepath.FromSlash(d)), env)
		if _, ok := errors.AsType[*NoFilesError](err); ok {
			continue // a directory of tests only has nothing to stub
		}
		if err != nil {
			return nil, err
		}
		// path.Join("", name) is name, for the tree's own package.
		prefix := strings.TrimPrefix(strings.TrimPrefix(d, dir), "/")
		for _, f := range files {
			f.Name = path.Join(prefix, f.Name)
			out = append(out, f)
		}
	}
	slices.SortFunc(out, func(a, b File) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}
