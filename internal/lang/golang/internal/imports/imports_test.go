package imports

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"slices"
	"strconv"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

func TestClassifyImport(t *testing.T) {
	l := &load.Module{Path: "example.com/fixture"}
	mod := &packages.Module{Path: "example.com/fixture", Main: true}
	for _, tc := range []struct {
		name string
		imp  *packages.Package
		want Class
	}{
		{"stdlib", &packages.Package{PkgPath: "strings"}, Stdlib},
		{"nested stdlib", &packages.Package{PkgPath: "net/http"}, Stdlib},
		{"internal module root", &packages.Package{PkgPath: "example.com/fixture", Module: mod}, Internal},
		{"internal", &packages.Package{PkgPath: "example.com/fixture/hub", Module: mod}, Internal},
		{"nested internal", &packages.Package{PkgPath: "example.com/fixture/hub/deep/er", Module: mod}, Internal},
		{"module path prefix without slash", &packages.Package{
			PkgPath: "example.com/fixturex",
			Module:  &packages.Module{Path: "example.com/fixturex"},
		}, External},
		{"external", &packages.Package{
			PkgPath: "golang.org/x/mod/modfile",
			Module:  &packages.Module{Path: "golang.org/x/mod", Version: "v0.1.0"},
		}, External},
		{"replaced local module", &packages.Package{
			PkgPath: "example.com/extmod",
			Module: &packages.Module{
				Path:    "example.com/extmod",
				Version: "v0.0.0",
				Replace: &packages.Module{Path: "../extmod"},
			},
		}, External},
		{"dotted path with no module", &packages.Package{PkgPath: "example.org/gopath"}, External},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(l, tc.imp); got != tc.want {
				t.Errorf("Classify(%q) = %d, want %d", tc.imp.PkgPath, got, tc.want)
			}
		})
	}
}

// TestClassifyImportStdlibNeverInternal measures a module whose path has no
// dot and matches a standard-library package, so the internal rule alone
// would claim it.
func TestClassifyImportStdlibNeverInternal(t *testing.T) {
	l := &load.Module{Path: "errors"}
	imp := &packages.Package{PkgPath: "errors"}
	if !isInternal(l, imp.PkgPath) {
		t.Fatalf("isInternal(%q) = false; the case does not exercise the ordering", imp.PkgPath)
	}
	if got := Classify(l, imp); got != Stdlib {
		t.Errorf("Classify(%q) = %d, want stdlib %d", imp.PkgPath, got, Stdlib)
	}
}

func TestImportsBlankDotAndDuplicates(t *testing.T) {
	fset := token.NewFileSet()
	srcs := []string{
		`package p
import (
	"strings"
	_ "embed"
	. "math"
	"example.com/m/q"
	"C"
)`,
		`package p
import (
	"strings"
	"example.com/m/q"
	"example.com/other"
)`,
	}
	files := make([]*ast.File, 0, len(srcs))
	for i, src := range srcs {
		f, err := parser.ParseFile(fset, "f"+strconv.Itoa(i)+".go", src, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing: %v", err)
		}
		files = append(files, f)
	}
	mod := &packages.Module{Path: "example.com/m", Main: true}
	p := &packages.Package{
		PkgPath: "example.com/m/p",
		Syntax:  files,
		Imports: map[string]*packages.Package{
			"strings":           {PkgPath: "strings"},
			"embed":             {PkgPath: "embed"},
			"math":              {PkgPath: "math"},
			"example.com/m/q":   {PkgPath: "example.com/m/q", Module: mod},
			"example.com/other": {PkgPath: "example.com/other", Module: &packages.Module{Path: "example.com/other"}},
		},
	}
	got := Count(&load.Module{Path: "example.com/m"}, p)
	// The blank embed and dot math imports count as stdlib imports beside
	// strings; "C" resolves to no package and counts nowhere.
	if got.Internal != 1 || got.External != 1 || got.Stdlib != 3 {
		t.Errorf("internal/external/stdlib = %d/%d/%d, want 1/1/3", got.Internal, got.External, got.Stdlib)
	}
	if !slices.Equal(got.Blank, []string{"embed"}) {
		t.Errorf("blank = %q, want [embed]", got.Blank)
	}
	if !slices.Equal(got.Dot, []string{"math"}) {
		t.Errorf("dot = %q, want [math]", got.Dot)
	}
}

func TestImportsFixture(t *testing.T) {
	l := loadFixture(t)

	t.Run("test-file imports excluded", func(t *testing.T) {
		const pkg = "example.com/fixture/tested"
		// The test variant imports "testing"; the metric must not see it.
		tv, ok := l.Tests[pkg]
		if !ok {
			t.Fatalf("no test variant for %s", pkg)
		}
		if _, ok := tv.Imports["testing"]; !ok {
			t.Fatalf("test variant of %s does not import testing; the case proves nothing", pkg)
		}
		got := Count(l, l.Pkgs[pkg])
		if got.Internal != 1 || got.External != 0 || got.Stdlib != 3 {
			t.Errorf("internal/external/stdlib = %d/%d/%d, want 1/0/3", got.Internal, got.External, got.Stdlib)
		}
	})

	for _, pkg := range l.Paths {
		t.Run("no blank or dot/"+path.Base(pkg), func(t *testing.T) {
			if got := Count(l, l.Pkgs[pkg]); len(got.Blank) != 0 || len(got.Dot) != 0 {
				t.Errorf("blank = %q, dot = %q, want none", got.Blank, got.Dot)
			}
		})
	}
}

// TestImported checks the one edge list behind fan-out and fan-in: every
// import spec whatever its name, in order of first appearance, one entry
// per package path, and nothing for an import with no loaded package.
func TestImported(t *testing.T) {
	s := newSynth(t)
	p := s.pkg("example.com/m/a", "example.com/m/b", "_ strings", ". example.com/m/b", "x/v=>example.com/m/vendor/x/v")
	p.Syntax = append(p.Syntax, s.pkg("example.com/m/a", "strings", "C").Syntax...)
	var got []string
	for _, imp := range Imported(p, p.Syntax) {
		got = append(got, imp.PkgPath)
	}
	if want := []string{"example.com/m/b", "strings", "example.com/m/vendor/x/v"}; !slices.Equal(got, want) {
		t.Errorf("Imported = %v, want %v", got, want)
	}
}
