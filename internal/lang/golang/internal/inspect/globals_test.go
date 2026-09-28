package inspect

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// parseForGlobals parses src as one file and wraps it in a package that
// carries only syntax, which is all globals reads.
func parseForGlobals(t *testing.T, src string) *packages.Package {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "src.go", src, 0)
	if err != nil {
		t.Fatalf("parsing snippet: %v", err)
	}
	return &packages.Package{Syntax: []*ast.File{f}}
}

func TestGlobalsSnippets(t *testing.T) {
	tests := []struct {
		name       string
		src        string
		globals    int
		initFuncs  int
		exported   []string
		unexported []string
	}{
		{
			name:       "single var",
			src:        "package p\nvar a = 1\n",
			globals:    1,
			unexported: []string{"a"},
		},
		{
			name:       "multi-name spec counts each name",
			src:        "package p\nvar a, B = 1, 2\n",
			globals:    2,
			exported:   []string{"B"},
			unexported: []string{"a"},
		},
		{
			name:       "grouped block with multi-name spec",
			src:        "package p\nvar (\n\ta, b = 1, 2\n)\n",
			globals:    2,
			unexported: []string{"a", "b"},
		},
		{
			name:       "grouped block counts names across specs",
			src:        "package p\nvar (\n\tX int\n\ty, z string\n)\n",
			globals:    3,
			exported:   []string{"X"},
			unexported: []string{"y", "z"},
		},
		{
			name:       "blank identifier excluded",
			src:        "package p\nvar _ = 1\nvar _, a = 1, 2\n",
			globals:    1,
			unexported: []string{"a"},
		},
		{
			name: "const block excluded",
			src:  "package p\nconst (\n\tA = 1\n\tb = 2\n)\nconst c = 3\n",
		},
		{
			name: "ten function-local vars contribute nothing",
			src: "package p\nfunc f() {\n" +
				"\tvar a0 int\n\tvar a1 int\n\tvar a2 int\n\tvar a3 int\n\tvar a4 int\n" +
				"\tvar a5 int\n\tvar a6 int\n\tvar a7 int\n\tvar a8 int\n\tvar a9 int\n" +
				"\t_, _, _, _, _, _, _, _, _, _ = a0, a1, a2, a3, a4, a5, a6, a7, a8, a9\n}\n",
		},
		{
			name:      "init without receiver counts",
			src:       "package p\nfunc init() {}\nfunc init() {}\n",
			initFuncs: 2,
		},
		{
			name: "method named init does not count",
			src:  "package p\ntype T struct{}\nfunc (T) init() {}\n",
		},
		{
			name: "type and import declarations ignored",
			src:  "package p\nimport \"fmt\"\ntype T int\nvar _ = fmt.Sprint\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Globals(nil, parseForGlobals(t, tt.src))
			if got.Globals != tt.globals || got.InitFuncs != tt.initFuncs {
				t.Errorf("globals=%d init_funcs=%d, want globals=%d init_funcs=%d",
					got.Globals, got.InitFuncs, tt.globals, tt.initFuncs)
			}
			if !slices.Equal(got.Exported, tt.exported) {
				t.Errorf("exported = %v, want %v", got.Exported, tt.exported)
			}
			if !slices.Equal(got.Unexported, tt.unexported) {
				t.Errorf("unexported = %v, want %v", got.Unexported, tt.unexported)
			}
		})
	}
}

func TestGlobalsHiddenBreakdown(t *testing.T) {
	l := loadFixture(t)
	got := Globals(l, l.Pkgs["example.com/fixture/hidden"])
	if want := []string{"limit", "events", "done", "counter"}; !slices.Equal(got.Unexported, want) {
		t.Errorf("unexported = %v, want %v", got.Unexported, want)
	}
	if len(got.Exported) != 0 {
		t.Errorf("exported = %v, want none", got.Exported)
	}
}

// TestGlobalsStdlibErrors loads the standard library errors package directly,
// outside the module loader. runtime.GOROOT is deprecated, so a toolchain
// without a usable GOROOT is detected by the load failing or returning no
// syntax, and the test skips.
func TestGlobalsStdlibErrors(t *testing.T) {
	cfg := &packages.Config{
		Mode: packages.NeedSyntax | packages.NeedFiles | packages.NeedName,
		Fset: token.NewFileSet(),
	}
	pkgs, err := packages.Load(cfg, "errors")
	if err != nil || len(pkgs) != 1 || len(pkgs[0].Errors) > 0 || len(pkgs[0].Syntax) == 0 {
		t.Skipf("loading stdlib errors: err=%v pkgs=%d", err, len(pkgs))
	}
	got := Globals(&load.Module{Fset: cfg.Fset}, pkgs[0])
	if got.Globals != 2 || got.InitFuncs != 0 {
		t.Errorf("globals=%d init_funcs=%d (exported %v, unexported %v), want globals=2 init_funcs=0",
			got.Globals, got.InitFuncs, got.Exported, got.Unexported)
	}
}
