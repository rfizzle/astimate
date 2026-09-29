package inspect

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// typeCheckForGlobals parses src as one file with comments, type-checks it
// as package example.com/p, importing standard-library packages from
// source, and wraps it in a package carrying its syntax, types and type
// information. With untyped set, the type information is dropped and only
// the package scope kept, the view a cgo package's source trees get.
func typeCheckForGlobals(t *testing.T, src string, untyped bool) *packages.Package {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing snippet: %v", err)
	}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	pkg, err := conf.Check("example.com/p", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatalf("type-checking snippet: %v", err)
	}
	p := &packages.Package{PkgPath: "example.com/p", Syntax: []*ast.File{f}, Types: pkg, TypesInfo: info}
	if untyped {
		p.TypesInfo = nil
	}
	return p
}

// countedGlobals runs the complexity walk and then Globals on p, as the
// extractor does, and returns the counted names.
func countedGlobals(p *packages.Package) []string {
	return Globals(nil, p, Complexity(nil, p).Written).Names
}

// TestGlobalsMutableState checks the three exclusions of SPEC.md 6.5
// (sentinel errors, embedded files, build information) and the state that
// still counts, each with full type information and again in the untyped
// view a cgo package's source trees get, where only the package scope
// resolves names. untypedNames overrides names where the two differ.
func TestGlobalsMutableState(t *testing.T) {
	tests := []struct {
		name         string
		src          string
		names        []string
		untypedNames []string
	}{
		{
			name: "errors.New sentinel",
			src:  "package p\nimport \"errors\"\nvar ErrNotFound = errors.New(\"not found\")\n",
		},
		{
			name: "fmt.Errorf with constant arguments",
			src:  "package p\nimport \"fmt\"\nvar ErrBad = fmt.Errorf(\"bad: %d\", 3)\n",
		},
		{
			name: "fmt.Errorf wrapping a variable counts",
			src: "package p\nimport (\n\t\"errors\"\n\t\"fmt\"\n)\n" +
				"var base = errors.New(\"base\")\nvar ErrX = fmt.Errorf(\"x: %w\", base)\n",
			names: []string{"ErrX"},
		},
		{
			name: "named constant argument",
			src:  "package p\nimport \"errors\"\nconst msg = \"m\"\nvar ErrM = errors.New(msg + \"!\")\n",
		},
		{
			name: "renamed import",
			src:  "package p\nimport errs \"errors\"\nvar ErrR = errs.New(\"r\")\n",
		},
		{
			name: "explicit error type and several names",
			src:  "package p\nimport \"errors\"\nvar ErrA, ErrB error = errors.New(\"a\"), errors.New(\"b\")\n",
		},
		{
			name:  "sentinel assigned in a function counts",
			src:   "package p\nimport \"errors\"\nvar ErrS = errors.New(\"s\")\nfunc Reset() { ErrS = nil }\n",
			names: []string{"ErrS"},
		},
		{
			name:  "error from another package counts",
			src:   "package p\nimport \"io\"\nvar ErrEOF = io.EOF\n",
			names: []string{"ErrEOF"},
		},
		{
			name:  "errors.New of a non-constant counts",
			src:   "package p\nimport (\n\t\"errors\"\n\t\"os\"\n)\nvar ErrArg = errors.New(os.Args[0])\n",
			names: []string{"ErrArg"},
		},
		{
			name:  "a local function named New counts",
			src:   "package p\nfunc New(s string) error { return nil }\nvar ErrL = New(\"l\")\n",
			names: []string{"ErrL"},
		},
		{
			name: "go:embed var",
			src:  "package p\nimport _ \"embed\"\n//go:embed default.yaml\nvar defaults []byte\n",
		},
		{
			name: "go:embed var in a group",
			src: "package p\nimport \"embed\"\nvar (\n\t// files holds the templates.\n\t//\n" +
				"\t//go:embed templates\n\tfiles embed.FS\n\tcache = map[string]string{}\n)\n",
			names: []string{"cache"},
		},
		{
			name: "go:embed var assigned counts",
			src: "package p\nimport _ \"embed\"\n//go:embed v.txt\nvar text string\n" +
				"func Set(s string) { text = s }\n",
			names: []string{"text"},
		},
		{
			name:  "go:embedded is not a directive",
			src:   "package p\n//go:embedded v.txt\nvar text []byte\n",
			names: []string{"text"},
		},
		{
			name: "build information never assigned",
			src: "package p\nvar version = \"dev\"\nvar (\n\tcommit string\n\tdirty bool\n" +
				"\tbuilt int64 = 1 << 10\n)\nfunc Version() string { return version + commit }\n",
		},
		{
			name:  "assigned in init counts",
			src:   "package p\nvar version = \"dev\"\nfunc init() { version = \"v1\" }\n",
			names: []string{"version"},
		},
		{
			name:  "assigned in a function counts",
			src:   "package p\nvar version = \"dev\"\nfunc Set(v string) { version = v }\n",
			names: []string{"version"},
		},
		{
			name:  "assigned in a function literal counts",
			src:   "package p\nvar debug bool\nfunc On() func() { return func() { debug = true } }\n",
			names: []string{"debug"},
		},
		{
			name:  "incremented counts",
			src:   "package p\nvar count int\nfunc Bump() int { count++; return count }\n",
			names: []string{"count"},
		},
		{
			name:  "compound assignment counts",
			src:   "package p\nvar total int\nfunc Add(n int) { total += n }\n",
			names: []string{"total"},
		},
		{
			name:  "address taken counts",
			src:   "package p\nvar limit = 3\nfunc Limit() *int { return &limit }\n",
			names: []string{"limit"},
		},
		{
			name:  "range assignment counts",
			src:   "package p\nvar last string\nfunc Scan(xs []string) { for _, last = range xs {} }\n",
			names: []string{"last"},
		},
		{
			name: "written in a var initializer counts",
			src: "package p\nvar version = \"dev\"\nvar set = func() { version = \"x\" }\n" +
				"var ptr = &version\n",
			names: []string{"version", "set", "ptr"},
		},
		{
			name:  "every name of a spec counts when one is written",
			src:   "package p\nvar major, minor = 1, 0\nfunc Bump() { minor++ }\n",
			names: []string{"major", "minor"},
		},
		{
			name: "shadowing local leaves the global uncounted",
			src: "package p\nvar version = \"dev\"\n" +
				"func f() string { version := \"x\"; version = \"y\"; return version }\n",
			untypedNames: []string{"version"},
		},
		{
			name:  "named basic type counts",
			src:   "package p\ntype Level int\nvar level Level = 1\n",
			names: []string{"level"},
		},
		{
			name:  "non-constant initializer counts",
			src:   "package p\nimport \"os\"\nvar argc = len(os.Args)\n",
			names: []string{"argc"},
		},
		{
			name: "maps, slices, mutexes, pointers, regexps, flags and funcs count",
			src: "package p\nimport (\n\t\"flag\"\n\t\"regexp\"\n\t\"sync\"\n)\nvar (\n" +
				"\tcfg = map[string]string{}\n\tnames []string\n\tmu sync.Mutex\n\tcur *int\n" +
				"\tword = regexp.MustCompile(`\\w+`)\n\tverbose = flag.Bool(\"v\", false, \"verbose\")\n" +
				"\thook = func() {}\n)\n",
			names: []string{"cfg", "names", "mu", "cur", "word", "verbose", "hook"},
		},
	}
	for _, tt := range tests {
		for _, untyped := range []bool{false, true} {
			name, want := tt.name, tt.names
			if untyped {
				name += " untyped"
				if tt.untypedNames != nil {
					want = tt.untypedNames
				}
			}
			t.Run(name, func(t *testing.T) {
				got := countedGlobals(typeCheckForGlobals(t, tt.src, untyped))
				if !slices.Equal(got, want) {
					t.Errorf("counted %v, want %v", got, want)
				}
			})
		}
	}
}

// TestWritesVisit checks the ast.Visitor that walks package-level var
// initializers: it records an address taken, by name without type
// information, continues into children and stops at a subtree's end.
func TestWritesVisit(t *testing.T) {
	w := &writes{}
	if got := w.Visit(&ast.UnaryExpr{Op: token.AND, X: ast.NewIdent("version")}); got != w {
		t.Errorf("Visit(&version) = %v, want the record itself", got)
	}
	if !w.names["version"] {
		t.Errorf("names = %v, want version recorded", w.names)
	}
	if got := w.Visit(nil); got != nil {
		t.Errorf("Visit(nil) = %v, want nil", got)
	}
}

// TestGlobalsWithoutTypes checks that a package carrying only syntax, with
// no package scope to type a name, counts every sentinel and build
// information spec: those exclusions need the types.
func TestGlobalsWithoutTypes(t *testing.T) {
	src := "package p\nimport \"errors\"\nvar ErrX = errors.New(\"x\")\nvar version = \"dev\"\n"
	got := countedGlobals(parseForGlobals(t, src))
	if want := []string{"ErrX", "version"}; !slices.Equal(got, want) {
		t.Errorf("counted %v, want %v", got, want)
	}
}

func TestGlobalsHiddenBreakdown(t *testing.T) {
	l := loadFixture(t)
	p := l.Pkgs["example.com/fixture/hidden"]
	got := Globals(l, p, Complexity(l, p).Written)
	if want := []string{"limit", "events", "done", "counter"}; !slices.Equal(got.Unexported, want) {
		t.Errorf("unexported = %v, want %v", got.Unexported, want)
	}
	if len(got.Exported) != 0 {
		t.Errorf("exported = %v, want none", got.Exported)
	}
}

// TestGlobalsImmutFixture checks the fixture package that covers each
// exclusion beside the state that still counts, loaded as the extractor
// loads it.
func TestGlobalsImmutFixture(t *testing.T) {
	l := loadFixture(t)
	p := l.Pkgs["example.com/fixture/immut"]
	got := Globals(l, p, Complexity(l, p).Written)
	if want := []string{"ErrWrapped", "cfg", "count", "mode"}; !slices.Equal(got.Names, want) {
		t.Errorf("names = %v, want %v", got.Names, want)
	}
}

// TestGlobalsStdlibErrors loads the standard library errors package directly,
// outside the module loader, with type information. Its sentinel
// ErrUnsupported, a call to its own New, is not counted; errorType, set from
// reflectlite, is. runtime.GOROOT is deprecated, so a toolchain without a
// usable GOROOT is detected by the load failing or returning no syntax, and
// the test skips.
func TestGlobalsStdlibErrors(t *testing.T) {
	cfg := &packages.Config{
		Mode: packages.NeedSyntax | packages.NeedFiles | packages.NeedName |
			packages.NeedTypes | packages.NeedTypesInfo,
		Fset: token.NewFileSet(),
	}
	pkgs, err := packages.Load(cfg, "errors")
	if err != nil || len(pkgs) != 1 || len(pkgs[0].Errors) > 0 || len(pkgs[0].Syntax) == 0 {
		t.Skipf("loading stdlib errors: err=%v pkgs=%d", err, len(pkgs))
	}
	m := &load.Module{Fset: cfg.Fset}
	got := Globals(m, pkgs[0], Complexity(m, pkgs[0]).Written)
	if got.Globals != 1 || got.InitFuncs != 0 || !slices.Equal(got.Names, []string{"errorType"}) {
		t.Errorf("globals=%d init_funcs=%d (names %v), want globals=1 (errorType) init_funcs=0",
			got.Globals, got.InitFuncs, got.Names)
	}
}

// BenchmarkGlobals runs Globals over every fixture package with the writes
// the complexity walk recorded.
func BenchmarkGlobals(b *testing.B) {
	l := loadFixture(b)
	written := make(map[string]map[string]bool, len(l.Paths))
	for _, path := range l.Paths {
		written[path] = Complexity(l, l.Pkgs[path]).Written
	}
	for b.Loop() {
		for _, path := range l.Paths {
			Globals(l, l.Pkgs[path], written[path])
		}
	}
}
