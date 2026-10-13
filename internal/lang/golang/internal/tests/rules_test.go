package tests

import (
	"go/ast"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"
)

// rulesRoot returns the absolute path of testdata/go/rules.
func rulesRoot(tb testing.TB) string {
	tb.Helper()
	return filepath.Join(filepath.Dir(fixtureRoot(tb)), "rules")
}

// TestUntestedExclusionRules checks the option-func, held-interface and
// test-support rules of SPEC.md 6.4 on testdata/go/rules: what each package
// counts, what each rule lists, and what changes with the rule switched off.
func TestUntestedExclusionRules(t *testing.T) {
	l := loadRoot(t, rulesRoot(t))
	const base = "example.com/rules/"
	for _, tc := range []struct {
		name, pkg                 string
		on                        rules
		names, option, held, supp []string
	}{
		{"option funcs", "opts", allRules, []string{"Apply", "WithRetry"}, []string{"Builder.WithName", "WithTimeout"}, nil, nil},
		{"option funcs off", "opts", allRules &^ ruleOptionFuncs, []string{"Apply", "Builder.WithName", "WithRetry", "WithTimeout"}, nil, nil, nil},
		{"interface a test holds", "rec", allRules, nil, nil, []string{"streamRecording.Emit"}, nil},
		{"interface rule off", "rec", allRules &^ ruleHeldInterfaces, []string{"streamRecording.Emit"}, nil, nil, nil},
		{"interface no test holds", "quiet", allRules, []string{"New", "fileSink.Emit"}, nil, nil, nil},
		{"test-support package", "support", allRules, nil, nil, nil, []string{"Fake.Do"}},
		{"test-support off", "support", allRules &^ ruleTestSupport, []string{"Fake.Do"}, nil, nil, nil},
		{"imported by non-test code", "shared", allRules, []string{"Thing.Go"}, nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := untested(l, new(Refs), l.Pkgs[base+tc.pkg], tc.on)
			for _, c := range []struct {
				what      string
				got, want []string
			}{
				{"names", got.Names, tc.names},
				{"option funcs", got.OptionFuncs, tc.option},
				{"interface methods", got.InterfaceMethods, tc.held},
				{"test support", got.TestSupport, tc.supp},
			} {
				if !slices.Equal(c.got, c.want) {
					t.Errorf("%s = %v, want %v", c.what, c.got, c.want)
				}
			}
			if got.Untested != len(tc.names) {
				t.Errorf("untested = %d, want %d", got.Untested, len(tc.names))
			}
		})
	}
}

// TestSupport checks Refs.Support on testdata/go/rules: the package only
// another package's tests use is test-support, and one a non-test file
// imports, or no test refers to, is not.
func TestSupport(t *testing.T) {
	l := loadRoot(t, rulesRoot(t))
	refs := new(Refs)
	for pkg, want := range map[string]bool{"support": true, "shared": false, "opts": false, "quiet": false} {
		if got := refs.Support(l, "example.com/rules/"+pkg); got != want {
			t.Errorf("Support(%s) = %v, want %v", pkg, got, want)
		}
	}
}

// TestUntestedPflagValue checks that pflag.Value and pflag.SliceValue are
// matched by method set, without the package being imported.
func TestUntestedPflagValue(t *testing.T) {
	const src = `package u

type Level int

func (Level) String() string    { return "" }
func (*Level) Set(string) error { return nil }
func (Level) Type() string      { return "level" }

type Names []string

func (*Names) Append(string) error     { return nil }
func (*Names) Replace([]string) error  { return nil }
func (Names) GetSlice() []string       { return nil }

type Partial struct{}

func (Partial) Type() string { return "partial" }

type Wrong int

func (Wrong) String() string   { return "" }
func (*Wrong) Set(int) error   { return nil }
func (Wrong) Type() string     { return "wrong" }
`
	l, p := checkUntestedSrc(t, map[string]string{"u.go": src})
	got := Untested(l, new(Refs), p)
	want := []string{"Partial.Type", "Wrong.Set", "Wrong.Type"}
	if !slices.Equal(got.Names, want) {
		t.Errorf("untested = %v, want %v", got.Names, want)
	}
}

func TestIsOptionFunc(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"one field", `return func(o *opts) { o.a = d }`, true},
		{"several and nested", `return func(o *opts) { o.a = d; o.b.c += 1 }`, true},
		{"append", `return func(o *opts) { o.l = append(o.l, d) }`, true},
		{"returns error", `return func(o *opts) error { o.a = d; return nil }`, false},
		{"validates", `return func(o *opts) { if d < 0 { return }; o.a = d }`, false},
		{"assigns a local", `return func(o *opts) { x := d; _ = x }`, false},
		{"assigns the argument", `return func(o *opts) { d = 1 }`, false},
		{"calls", `return func(o *opts) { o.f() }`, false},
		{"two statements", `x := d; return func(o *opts) { o.a = x }`, false},
		{"no literal", `return nil`, false},
		{"empty literal", `return func(o *opts) {}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package u\n\ntype opts struct{ a any; l []int; b struct{ c int } }\nfunc (*opts) f() {}\nfunc With(d int) any {\n" + tc.body + "\n}\n"
			l, p := checkUntestedSrc(t, map[string]string{"u.go": src})
			_ = l
			opt := isOptionFunc(findDecl(t, p, "With"))
			if opt != tc.want {
				t.Errorf("isOptionFunc = %v, want %v", opt, tc.want)
			}
		})
	}
}

// findDecl returns the func declaration called name in p.
func findDecl(t *testing.T, p *packages.Package, name string) *ast.FuncDecl {
	t.Helper()
	for _, f := range p.Syntax {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name {
				return fd
			}
		}
	}
	t.Fatalf("no func %s", name)
	return nil
}
