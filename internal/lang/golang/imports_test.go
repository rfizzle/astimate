package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"slices"
	"strconv"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestClassifyImport(t *testing.T) {
	l := &loaded{modulePath: "example.com/fixture"}
	mod := &packages.Module{Path: "example.com/fixture", Main: true}
	for _, tc := range []struct {
		name string
		imp  *packages.Package
		want importClass
	}{
		{"stdlib", &packages.Package{PkgPath: "strings"}, importStdlib},
		{"nested stdlib", &packages.Package{PkgPath: "net/http"}, importStdlib},
		{"internal module root", &packages.Package{PkgPath: "example.com/fixture", Module: mod}, importInternal},
		{"internal", &packages.Package{PkgPath: "example.com/fixture/hub", Module: mod}, importInternal},
		{"nested internal", &packages.Package{PkgPath: "example.com/fixture/hub/deep/er", Module: mod}, importInternal},
		{"module path prefix without slash", &packages.Package{
			PkgPath: "example.com/fixturex",
			Module:  &packages.Module{Path: "example.com/fixturex"},
		}, importExternal},
		{"external", &packages.Package{
			PkgPath: "golang.org/x/mod/modfile",
			Module:  &packages.Module{Path: "golang.org/x/mod", Version: "v0.1.0"},
		}, importExternal},
		{"replaced local module", &packages.Package{
			PkgPath: "example.com/extmod",
			Module: &packages.Module{
				Path:    "example.com/extmod",
				Version: "v0.0.0",
				Replace: &packages.Module{Path: "../extmod"},
			},
		}, importExternal},
		{"dotted path with no module", &packages.Package{PkgPath: "example.org/gopath"}, importExternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyImport(l, tc.imp); got != tc.want {
				t.Errorf("classifyImport(%q) = %d, want %d", tc.imp.PkgPath, got, tc.want)
			}
		})
	}
}

// TestClassifyImportStdlibNeverInternal measures a module whose path has no
// dot and matches a standard-library package, so the internal rule alone
// would claim it.
func TestClassifyImportStdlibNeverInternal(t *testing.T) {
	l := &loaded{modulePath: "errors"}
	imp := &packages.Package{PkgPath: "errors"}
	if !isInternal(l, imp.PkgPath) {
		t.Fatalf("isInternal(%q) = false; the case does not exercise the ordering", imp.PkgPath)
	}
	if got := classifyImport(l, imp); got != importStdlib {
		t.Errorf("classifyImport(%q) = %d, want stdlib %d", imp.PkgPath, got, importStdlib)
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
	got := imports(&loaded{modulePath: "example.com/m"}, p)
	if got.internal != 1 || got.external != 1 || got.stdlib != 1 {
		t.Errorf("internal/external/stdlib = %d/%d/%d, want 1/1/1", got.internal, got.external, got.stdlib)
	}
	if !slices.Equal(got.blank, []string{"embed"}) {
		t.Errorf("blank = %q, want [embed]", got.blank)
	}
	if !slices.Equal(got.dot, []string{"math"}) {
		t.Errorf("dot = %q, want [math]", got.dot)
	}
}

func TestImportsFixture(t *testing.T) {
	l := loadFixture(t)

	t.Run("test-file imports excluded", func(t *testing.T) {
		const pkg = "example.com/fixture/tested"
		// The test variant imports "testing"; the metric must not see it.
		tv, ok := l.tests[pkg]
		if !ok {
			t.Fatalf("no test variant for %s", pkg)
		}
		if _, ok := tv.Imports["testing"]; !ok {
			t.Fatalf("test variant of %s does not import testing; the case proves nothing", pkg)
		}
		got := imports(l, l.pkgs[pkg])
		if got.internal != 1 || got.external != 0 || got.stdlib != 3 {
			t.Errorf("internal/external/stdlib = %d/%d/%d, want 1/0/3", got.internal, got.external, got.stdlib)
		}
	})

	for _, pkg := range l.paths {
		t.Run("no blank or dot/"+path.Base(pkg), func(t *testing.T) {
			if got := imports(l, l.pkgs[pkg]); len(got.blank) != 0 || len(got.dot) != 0 {
				t.Errorf("blank = %q, dot = %q, want none", got.blank, got.dot)
			}
		})
	}
}
