package golang

import (
	"maps"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestSourceSyntaxCgo checks that a cgo package is measured from the files
// in GoFiles, not the files cgo generated into the build cache: the trees
// sourceSyntax returns and the per-file line counts of size name exactly
// GoFiles, while the type-checked p.Syntax does not. A pure Go package keeps
// p.Syntax itself.
func TestSourceSyntaxCgo(t *testing.T) {
	if !haveCgo(t) {
		t.Skip("cgo or its C compiler is unavailable")
	}
	l, err := loadModule(&packages.Config{Dir: cgoRoot(t)}, packages.Load)
	if err != nil {
		t.Fatalf("loadModule: %v", err)
	}

	native := l.pkgs["example.com/cgo/native"]
	if syntaxIsSource(l, native) {
		t.Fatalf("native: p.Syntax is its source files %v; the fixture no longer exercises cgo", native.GoFiles)
	}
	names := make([]string, 0, len(native.GoFiles))
	for _, f := range sourceSyntax(l, native) {
		names = append(names, l.fset.File(f.FileStart).Name())
	}
	if !slices.Equal(names, native.GoFiles) {
		t.Errorf("native: sourceSyntax files = %v, want GoFiles %v", names, native.GoFiles)
	}
	sz, err := size(l, native, osFiles{})
	if err != nil {
		t.Fatalf("size(native): %v", err)
	}
	if got := slices.Sorted(maps.Keys(sz.perFile)); !slices.Equal(got, native.GoFiles) || len(got) != sz.files {
		t.Errorf("native: perFile keys = %v, files = %d, want GoFiles %v", got, sz.files, native.GoFiles)
	}

	user := l.pkgs["example.com/cgo/user"]
	if got := sourceSyntax(l, user); !slices.Equal(got, user.Syntax) {
		t.Errorf("user: sourceSyntax is not p.Syntax for a package without cgo")
	}
}
