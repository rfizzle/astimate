package stdiface

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// TestStdInterfacesClosedList checks that every method name of the closed
// list of SPEC.md 6.4 is indexed, so a failed type-check of the embedded
// source cannot silently drop the plain interfaces.
func TestStdInterfacesClosedList(t *testing.T) {
	s := New()
	for _, name := range []string{
		"Error", "String", "GoString", "MarshalText", "UnmarshalText",
		"MarshalBinary", "UnmarshalBinary", "MarshalJSON", "UnmarshalJSON",
		"Len", "Less", "Swap", "Push", "Pop", "Read", "Write", "Close",
		"Scan", "Set", "Unwrap", "Is", "As",
	} {
		if len(s.plain[name]) == 0 {
			t.Errorf("method %s is not on the closed list", name)
		}
	}
	for _, name := range []string{"Format", "ReadFrom", "WriteTo", "ServeHTTP", "Value", "LogValue"} {
		if len(s.named[name]) == 0 {
			t.Errorf("method %s is not on the closed list", name)
		}
	}
}

// TestCovers checks Covers on methods that implement a listed interface,
// pflag.Value among them, and on methods that only share a name.
func TestCovers(t *testing.T) {
	const src = `package u

type E struct{}

func (E) Error() string { return "" }

type V struct{}

func (V) Type() string { return "" }
func (*V) Set(string) error { return nil }
func (V) String() string { return "" }

type P struct{}

func (P) Type() string { return "" }

type W struct{}

func (W) Error() int { return 0 }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "u.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := (&types.Config{}).Check("u", fset, []*ast.File{f}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := New()
	for _, tc := range []struct {
		typ, method string
		want        bool
	}{
		{"E", "Error", true},
		{"V", "Type", true},
		{"V", "Set", true},
		{"P", "Type", false},
		{"W", "Error", false},
	} {
		named := pkg.Scope().Lookup(tc.typ).Type()
		obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(named), true, pkg, tc.method)
		fn, ok := obj.(*types.Func)
		if !ok {
			t.Fatalf("no method %s.%s", tc.typ, tc.method)
		}
		if NamedOf(types.NewPointer(named)) == nil || !Implements(named, types.Universe.Lookup("any").Type().Underlying().(*types.Interface)) {
			t.Errorf("%s: NamedOf or Implements fails on a named type", tc.typ)
		}
		if got := s.Covers(fn); got != tc.want {
			t.Errorf("Covers(%s.%s) = %v, want %v", tc.typ, tc.method, got, tc.want)
		}
	}
}
