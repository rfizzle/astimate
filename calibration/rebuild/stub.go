package main

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
)

// stubPanic is the message every stubbed body panics with.
const stubPanic = "not implemented"

// stubBody replaces every function and method body.
const stubBody = "{\n\tpanic(\"" + stubPanic + "\")\n}"

// StubFile is one stubbed source file.
type StubFile struct {
	// Name is the file's base name in the package directory.
	Name string
	// Data is the stubbed source.
	Data []byte
}

// StubPackage returns the stubbed form of every non-test .go file in dir,
// sorted by name, under the signatures strategy: each function or method
// body, init included, becomes panic("not implemented"), comments inside
// the bodies go with them, everything outside a body is kept byte for byte,
// and the result is gofmt-formatted with the imports no file uses any more
// removed. Files of every build configuration are stubbed, and their
// //go:build lines are kept. Declarations without a body (assembly or
// linkname) are kept as they are. The output depends only on the files'
// contents: the same directory yields byte-identical results.
func StubPackage(dir string) ([]StubFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("stubbing %s: %w", dir, err)
	}
	var out []StubFile
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
		data, err := stubSource(path, src)
		if err != nil {
			return nil, err
		}
		out = append(out, StubFile{Name: name, Data: data})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("stubbing %s: no non-test Go files", dir)
	}
	slices.SortFunc(out, func(a, b StubFile) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// stubSource stubs one file's source. path is used for error messages and
// lets imports.Process read the package's sibling files, so an import is
// only dropped when nothing in the stubbed file refers to it.
func stubSource(path string, src []byte) ([]byte, error) {
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
		b.WriteString(stubBody)
		last = end
	}
	b.Write(src[last:])
	// Stubbing only deletes code and panic needs no import, so the file
	// refers to nothing new and imports.Process only removes imports; it
	// adds one only for an unresolved name, which a compiling package does
	// not have. TestStubSource pins that no import is added.
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

// WriteStub writes files into dir, replacing the originals.
func WriteStub(dir string, files []StubFile) error {
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
// (StubPackage sorts them by name): each file contributes its name, a
// newline, its length in decimal, a newline and its bytes.
func TreeHash(files []StubFile) string {
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Name + "\n" + strconv.Itoa(len(f.Data)) + "\n"))
		h.Write(f.Data)
	}
	return hex.EncodeToString(h.Sum(nil))
}
