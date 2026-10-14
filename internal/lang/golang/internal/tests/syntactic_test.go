package tests

import (
	"path/filepath"
	"slices"
	"testing"
)

// TestSkippedTestRefs loads testdata/go/skipped, whose skipped package
// remote has the only test calling base.New and (*base.Counter).Add, and
// checks that both read as tested, so base has tests anywhere, while
// Value and Checksum, which no test names, stay untested.
func TestSkippedTestRefs(t *testing.T) {
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	m := loadRoot(t, filepath.Join(filepath.Dir(fixtureRoot(t)), "skipped"))
	if len(m.SkippedPackages) != 1 {
		t.Fatalf("skipped = %+v, want remote alone", m.SkippedPackages)
	}
	var refs Refs
	c := Untested(m, &refs, m.Pkgs["example.com/skipped/base"])
	if want := []string{"Checksum", "Value"}; !slices.Equal(c.Names, want) {
		t.Errorf("untested = %v, want %v", c.Names, want)
	}
	if c.Referenced != 2 || !c.TestedAnywhere() {
		t.Errorf("referenced = %d, want 2 (New and Counter.Add), tested anywhere", c.Referenced)
	}
	if refs.Support(m, "example.com/skipped/base") {
		t.Error("base reads as a test-support package, though app and remote import it")
	}
}
