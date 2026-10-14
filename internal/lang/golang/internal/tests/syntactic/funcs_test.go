package syntactic

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// TestFuncs loads testdata/go/skipped, whose skipped package remote has a
// test file calling base.New and Add on the Counter it returns, and checks
// that Funcs returns those two and nothing else.
func TestFuncs(t *testing.T) {
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "..", "..", "testdata", "go", "skipped"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := load.Load(&packages.Config{Dir: root}, packages.Load)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, fn := range Funcs(m) {
		got = append(got, fn.FullName())
	}
	slices.Sort(got)
	want := []string{"(*example.com/skipped/base.Counter).Add", "example.com/skipped/base.New"}
	if !slices.Equal(got, want) {
		t.Errorf("Funcs = %v, want %v", got, want)
	}
}
