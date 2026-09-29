package tests

import (
	"path/filepath"
	"slices"
	"testing"
)

// TestUntestedOtherPackagesTests checks that a test file of another package
// of the module refers to a func or method as the package's own tests
// would, directly and through an interface, and that without that
// refinement only the package's own tests count.
func TestUntestedOtherPackagesTests(t *testing.T) {
	l := loadRoot(t, refsRoot(t))
	for _, tc := range []struct {
		name, pkg string
		on        rules
		want      []string
	}{
		// Lib is called and Impl.Area dispatched through refs.Shape from
		// user's test; Unused is referenced from no test file anywhere.
		{"module-wide", "example.com/refs/lib", allRules, []string{"Unused"}},
		{"own tests only", "example.com/refs/lib", ruleStdInterfaces, []string{"Impl.Area", "Lib", "Unused"}},
		// user's test calls refs.Shape.Area, but no test binary holds an
		// unseen.Tri, so the dispatch does not reach it.
		{"dispatch needs the package in the test binary", "example.com/refs/unseen", allRules, []string{"Tri.Area"}},
		// The refs package's own tests already reach everything but Never.
		{"own tests already cover", "example.com/refs/refs", allRules, []string{"Never"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := untested(l, new(Refs), l.Pkgs[tc.pkg], tc.on)
			if got.Untested != len(tc.want) || !slices.Equal(got.Names, tc.want) {
				t.Errorf("untested = %d %v, want %d %v", got.Untested, got.Names, len(tc.want), tc.want)
			}
		})
	}
}

// TestUntestedStdInterfaces checks the closed list of SPEC.md 6.4: a method
// counts as referenced only when its receiver type, or a pointer to it,
// implements the listed interface or convention, so the same name with
// another signature, or one method of a larger interface, still counts.
func TestUntestedStdInterfaces(t *testing.T) {
	const src = `package u

import (
	"database/sql/driver"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

type NotFound struct{ err error }

func (e *NotFound) Error() string        { return "not found" }
func (e *NotFound) Unwrap() error        { return e.err }
func (e *NotFound) Is(target error) bool { return target == e }
func (e *NotFound) As(target any) bool   { return false }

type Multi []error

func (m Multi) Error() string   { return "multi" }
func (m Multi) Unwrap() []error { return m }

type Code int

func (c Code) Error(verbose bool) string       { return "code" }
func (c Code) Unwrap() int                     { return 0 }
func (c Code) String() string                  { return "code" }
func (c Code) GoString() string                { return "Code" }
func (c Code) Len() int                        { return 0 }
func (c Code) Format(f fmt.State, verb rune)   {}
func (c Code) LogValue() slog.Value            { return slog.Value{} }
func (c Code) Value() (driver.Value, error)    { return nil, nil }

type Text struct{}

func (*Text) MarshalText() ([]byte, error)   { return nil, nil }
func (*Text) UnmarshalText([]byte) error     { return nil }
func (*Text) MarshalBinary() ([]byte, error) { return nil, nil }
func (*Text) UnmarshalBinary([]byte) error   { return nil }
func (*Text) MarshalJSON() ([]byte, error)   { return nil, nil }
func (*Text) UnmarshalJSON([]byte) error     { return nil }
func (*Text) Scan(src any) error             { return nil }

type List []int

func (l List) Len() int           { return len(l) }
func (l List) Less(i, j int) bool { return l[i] < l[j] }
func (l List) Swap(i, j int)      { l[i], l[j] = l[j], l[i] }
func (l *List) Push(x any)        {}
func (l *List) Pop() any          { return nil }

type Stream struct{}

func (Stream) Read(p []byte) (int, error)                   { return 0, nil }
func (Stream) Write(p []byte) (int, error)                  { return 0, nil }
func (Stream) Close() error                                 { return nil }
func (Stream) ReadFrom(r io.Reader) (int64, error)          { return 0, nil }
func (Stream) WriteTo(w io.Writer) (int64, error)           { return 0, nil }
func (Stream) ServeHTTP(http.ResponseWriter, *http.Request) {}

type Flag struct{}

func (*Flag) String() string   { return "" }
func (*Flag) Set(string) error { return nil }

type Odd struct{}

func (Odd) Write(p string) (int, error) { return 0, nil }
func (Odd) Close() int                  { return 0 }
func (Odd) Swap(i, j int)               {}
func (Odd) Format(s string)             {}
func (Odd) ServeHTTP(w io.Writer)       {}

type Box[T any] struct{ v T }

func (Box[T]) Error() string { return "box" }
func (b Box[T]) Get() T      { return b.v }
`
	l, p := checkUntestedSrc(t, map[string]string{"u.go": src})
	// Code.Error takes an argument and Code.Unwrap returns int, so Code is
	// neither an error nor unwrappable; Code.Len is one method of
	// sort.Interface. Odd's methods have listed names with other
	// signatures. Box.Get is on no list; Box[T].Error implements error.
	want := []string{"Box.Get", "Code.Error", "Code.Len", "Code.Unwrap", "Odd.Close", "Odd.Format", "Odd.ServeHTTP", "Odd.Swap", "Odd.Write"}
	got := untested(l, new(Refs), p, allRules)
	if got.Untested != len(want) || !slices.Equal(got.Names, want) {
		t.Errorf("untested = %d %v, want %d %v", got.Untested, got.Names, len(want), want)
	}
	// Without the refinement every method counts, since the package has
	// no test files.
	if off := untested(l, new(Refs), p, ruleOtherTests); off.Untested != 41 {
		t.Errorf("untested without the closed list = %d %v, want 41", off.Untested, off.Names)
	}
}

// TestStdInterfacesClosedList checks that every method name of the closed
// list of SPEC.md 6.4 is indexed, so a failed type-check of the embedded
// source cannot silently drop the plain interfaces.
func TestStdInterfacesClosedList(t *testing.T) {
	s := newStdInterfaces()
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

// TestRefsBuiltOnce checks that the module-wide index is built on first use
// and shared by every later package and by Referenced.
func TestRefsBuiltOnce(t *testing.T) {
	l := loadRoot(t, refsRoot(t))
	var r Refs
	if r.builds != 0 || r.direct != nil {
		t.Fatal("a new Refs is built before first use")
	}
	for _, path := range l.Paths {
		Untested(l, &r, l.Pkgs[path])
	}
	for _, tc := range []struct {
		pkg, key string
		want     bool
	}{
		{"example.com/refs/lib", "Lib", true},
		{"example.com/refs/lib", "Unused", false},
		{"example.com/refs/refs", "Counter.Inc", true},
		// Interface dispatch is not a Uses reference.
		{"example.com/refs/lib", "Impl.Area", false},
	} {
		if got := r.Referenced(l, tc.pkg, tc.key); got != tc.want {
			t.Errorf("Referenced(%s, %s) = %v, want %v", tc.pkg, tc.key, got, tc.want)
		}
	}
	if r.builds != 1 {
		t.Errorf("index built %d times, want 1", r.builds)
	}
}

// BenchmarkRefsBuild measures the one-time module-wide index build on the
// fixture and on this repository.
func BenchmarkRefsBuild(b *testing.B) {
	fixture := fixtureRoot(b)
	for _, bc := range []struct{ name, dir string }{
		{"fixture", fixture},
		// fixture is <repo>/testdata/go/fixture.
		{"self", filepath.Dir(filepath.Dir(filepath.Dir(fixture)))},
	} {
		b.Run(bc.name, func(b *testing.B) {
			l := loadRoot(b, bc.dir)
			b.ReportAllocs()
			for b.Loop() {
				new(Refs).build(l)
			}
		})
	}
}
