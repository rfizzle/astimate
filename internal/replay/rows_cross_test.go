package replay

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/score"
)

// updateFlag names the flag that makes the golden tests of this package
// rewrite their golden files under testdata. Use it only after an
// intentional change to the rows, then read the result.
const updateFlag = "update"

// TestMain defines the -update flag before the test binary parses flags;
// the golden tests read it back with flag.Lookup, so no package variable
// holds it.
func TestMain(m *testing.M) {
	flag.Bool(updateFlag, false, "rewrite the replay golden files")
	flag.Parse()
	os.Exit(m.Run())
}

// compareGolden compares got with the golden file at golden, first
// rewriting it under -update.
func compareGolden(t *testing.T, golden, got string) {
	t.Helper()
	if flag.Lookup(updateFlag).Value.String() == "true" {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run with -%s to create it)", err, updateFlag)
	}
	if got != string(want) {
		t.Errorf("output differs from %s; rerun with -%s and read the diff:\n%s", golden, updateFlag, got)
	}
}

// TestReplayCrossAdded replays a commit that copies a function into a
// second package and checks the module rows' cross_added: the copy's set
// of packages, with no baseline block on it, against the parent; nothing
// recorded on the root commit, judged against an empty baseline whose
// count the gate judges in full.
func TestReplayCrossAdded(t *testing.T) {
	goEnv(t)
	repo, hashes := fixtureRepo(t, []fixtureCommit{
		{files: map[string]string{"go.mod": goMod, "a/a.go": "package a\n" + dupFunc("first")}, message: "feat: add package a"},
		{files: map[string]string{"b/b.go": "package b\n" + dupFunc("second")}, message: "feat: copy it into b"},
	})
	out := filepath.Join(t.TempDir(), "out")
	if code, log := replayT(t, "--repo", repo, "--out", out); code != exitOK {
		t.Fatalf("exit %d: %s", code, log)
	}
	pkgs, err := readRows[packageRow](filepath.Join(out, packagesFile))
	if err != nil {
		t.Fatal(err)
	}
	if root := packageRowOf(t, pkgs, hashes[0], "<module>"); root.CrossAdded != nil {
		t.Errorf("root commit module row cross_added %+v, want none", root.CrossAdded)
	}
	if a := packageRowOf(t, pkgs, hashes[1], "b"); a.CrossAdded != nil {
		t.Errorf("package row cross_added %+v, want none", a.CrossAdded)
	}
	mod := packageRowOf(t, pkgs, hashes[1], "<module>")
	data, err := json.MarshalIndent(map[string]any{"cross_added": mod.CrossAdded}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	compareGolden(t, filepath.Join("testdata", "cross-added.golden"), string(data)+"\n")
}

// TestPackageRowCrossAddedEmpty checks that a module row whose change added
// no block at any floor records an empty cross_added, which a validation
// reads as zero added blocks, while a row without groups leaves it out.
func TestPackageRowCrossAddedEmpty(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		groups []score.CrossGroup
		want   string
	}{
		{groups: []score.CrossGroup{}, want: `"cross_added":[]`},
		{groups: nil, want: ""},
	} {
		row := newPackageRow("c", &report.CheckedPackage{Report: report.Report{PackagePath: "<module>"}, CrossGroups: tt.groups})
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(string(data), `"cross_added"`); got != (tt.want != "") || !strings.Contains(string(data), tt.want) {
			t.Errorf("groups %v: row JSON %s, want %q", tt.groups, data, tt.want)
		}
	}
}
