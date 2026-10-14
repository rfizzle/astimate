package dup

import (
	"go/ast"
	"go/parser"
	"go/token"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
)

// appendSkipped scans the non-test, non-generated files of the skipped
// package sk, read through src, into sink, closing each file, as
// appendPackage does for a loaded package. The load holds no syntax for a
// skipped package, so each file is parsed here, from the bytes read, for
// its generated header and its token shapes (SPEC.md 6.3); a file that
// does not parse is left out, since a skipped package may fail on a syntax
// error and must not cost the module its stream.
func (z *dupTokenizer) appendSkipped(fs *token.FileSet, sk load.SkippedFiles, src load.FileSource, sink tokenSink) error {
	pfs := token.NewFileSet()
	for _, name := range sk.GoFiles {
		data, err := src.Read(name)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(pfs, name, data, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil || ast.IsGenerated(f) {
			continue
		}
		sh := &shapes{spans: shapeSpans(pfs.File(f.FileStart), f)}
		if err := z.scan(fs, name, data, sh, sink); err != nil {
			return err
		}
	}
	return nil
}
