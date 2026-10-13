package engine

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateFlag names the flag that makes the golden tests of this package
// rewrite their golden files under testdata. Use it only after an
// intentional change to the output, then read the result.
const updateFlag = "update"

// TestMain defines the -update flag before the test binary parses flags;
// the golden tests read it back with flag.Lookup, so no package variable
// holds it.
func TestMain(m *testing.M) {
	flag.Bool(updateFlag, false, "rewrite the engine golden files")
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
