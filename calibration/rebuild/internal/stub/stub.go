// Package stub implements the signatures stub strategy of SPEC.md 11.2: it
// replaces every function and method body of a package's non-test files
// with panic("not implemented"), keeping everything else byte for byte.
package stub

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/imports"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
)

// Panic is the message every stubbed body panics with.
const Panic = "not implemented"

// body replaces every function and method body.
const body = "{\n\tpanic(\"" + Panic + "\")\n}"

// File is one stubbed source file.
type File struct {
	// Name is the file's base name in the package directory.
	Name string
	// Data is the stubbed source.
	Data []byte
}

// Package returns the stubbed form of every non-test .go file in dir,
// sorted by name, under the signatures strategy: each function or method
// body, init included, becomes panic("not implemented"), comments inside
// the bodies go with them, everything outside a body is kept byte for byte,
// and the result is gofmt-formatted with the imports no file uses any more
// removed. Files of every build configuration are stubbed, and their
// //go:build lines are kept. Declarations without a body (assembly or
// linkname) are kept as they are. The output depends only on the files'
// contents: the same directory yields byte-identical results.
func Package(dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("stubbing %s: %w", dir, err)
	}
	var out []File
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
		data, err := source(path, src)
		if err != nil {
			return nil, err
		}
		out = append(out, File{Name: name, Data: data})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("stubbing %s: no non-test Go files", dir)
	}
	slices.SortFunc(out, func(a, b File) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// source stubs one file's source. path is used for error messages and lets
// imports.Process read the package's sibling files, so an import is only
// dropped when nothing in the stubbed file refers to it.
func source(path string, src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("stubbing %s: %w", path, err)
	}
	var b strings.Builder
	b.Grow(len(src))
	last := 0
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		start := fset.Position(fn.Body.Lbrace).Offset
		end := fset.Position(fn.Body.Rbrace).Offset + 1
		b.Write(src[last:start])
		b.WriteString(body)
		last = end
	}
	b.Write(src[last:])
	// Stubbing only deletes code and panic needs no import, so the file
	// refers to nothing new and imports.Process only removes imports; it
	// adds one only for an unresolved name, which a compiling package does
	// not have. TestSource pins that no import is added.
	out, err := imports.Process(path, []byte(b.String()), &imports.Options{
		Comments:  true,
		TabIndent: true,
		TabWidth:  8,
	})
	if err != nil {
		return nil, fmt.Errorf("stubbing %s: formatting: %w", path, err)
	}
	return out, nil
}

// Write writes files into dir, replacing the originals.
func Write(dir string, files []File) error {
	for _, f := range files {
		path := filepath.Join(dir, f.Name)
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
// (Package sorts them by name): each file contributes its name, a newline,
// its length in decimal, a newline and its bytes.
func TreeHash(files []File) string {
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Name + "\n" + strconv.Itoa(len(f.Data)) + "\n"))
		h.Write(f.Data)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Apply stubs the package of e in the clone at root and checks the result
// against e.StubSHA256, so a runner starts from the tree the selection
// verified.
func Apply(root string, e *definition.Experiment) error {
	pkgDir := filepath.Join(root, filepath.FromSlash(e.Dir))
	files, err := Package(pkgDir)
	if err != nil {
		return err
	}
	if got := TreeHash(files); got != e.StubSHA256 {
		return fmt.Errorf("stubbing %s: tree hash %s, want %s (different toolchain or tree?)", e.Package, got, e.StubSHA256)
	}
	return Write(pkgDir, files)
}
