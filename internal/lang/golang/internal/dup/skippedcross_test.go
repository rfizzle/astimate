package dup

import (
	"path/filepath"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// TestCrossWithSkipped loads testdata/go/skipped, whose skipped package
// remote holds a copy of base.Checksum, and checks that the copy is a
// cross-package block naming both packages, that base counts it and that
// remote, which has no row, gets no count.
func TestCrossWithSkipped(t *testing.T) {
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	root := filepath.Join(filepath.Dir(fixtureRoot(t)), "skipped")
	m, err := load.Load(&packages.Config{Dir: root}, packages.Load)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.SkippedPackages) != 1 {
		t.Fatalf("skipped = %+v, want remote alone", m.SkippedPackages)
	}
	var memo Memo
	c, err := memo.Cross(m, load.OSFiles{}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Blocks) != 1 {
		t.Fatalf("blocks = %+v, want the one copy of Checksum", c.Blocks)
	}
	occ := c.Blocks[0].Occurrences
	if len(occ) != 2 || occ[0].Package != "example.com/skipped/base" || occ[0].File != "base/base.go" ||
		occ[1].Package != "example.com/skipped/remote" || occ[1].File != "remote/remote.go" {
		t.Errorf("occurrences = %+v, want base/base.go and remote/remote.go", occ)
	}
	if c.Blocks[0].Kind != "code" {
		t.Errorf("kind = %s, want code", c.Blocks[0].Kind)
	}
	if len(c.PerPkg) != 1 || c.PerPkg["example.com/skipped/base"] != 1 {
		t.Errorf("per package = %v, want base alone with 1", c.PerPkg)
	}
}
