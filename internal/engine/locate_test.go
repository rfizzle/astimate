package engine

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
)

// degradedDir is the degraded copy of the fixture: its tested package adds
// a duplicate function, an untested export, a global and a complex function.
const degradedDir = "../../testdata/go/fixture-degraded"

// TestCheckLocatesDegradedFindings checks the degraded fixture against a
// baseline of the pristine one: each violation of its tested package is
// located on the line the degraded copy added, module-relative, and the
// check carries the module root's directory in its repository.
func TestCheckLocatesDegradedFindings(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	fx, err := LoadTarget(fixtureDir, TargetOptions{Tokenizer: TokenizerEst})
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := baseline.Collect(t.Context(), fx.Ext, fx.Mod)
	if err != nil {
		t.Fatal(err)
	}
	funcs, err := baseline.CollectFunctions(t.Context(), fx.Ext, fx.Mod, pkgs)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := baseline.WriteContents(path, baseline.Contents{
		Ref: "fixture", ModulePath: fx.Mod.ModulePath, Tokenizer: TokenizerEst, Packages: pkgs, Functions: funcs,
	}); err != nil {
		t.Fatal(err)
	}

	tg, err := LoadTarget(degradedDir, TargetOptions{Tokenizer: TokenizerEst})
	if err != nil {
		t.Fatal(err)
	}
	c, failed, err := Check(t.Context(), tg, CheckOptions{BaselineFile: path, Packages: []string{tg.Mod.ModulePath + "/tested"}})
	if err != nil || len(failed) != 0 {
		t.Fatalf("Check = (%v, %v), want no error", failed, err)
	}
	if want := baseline.RepoDir(t.Context(), tg.Mod.Root); c.ModuleDir != want {
		t.Errorf("ModuleDir = %q, want %q", c.ModuleDir, want)
	}
	if want := "testdata/go/fixture-degraded"; c.ModuleDir != "" && c.ModuleDir != want {
		t.Errorf("ModuleDir = %q, want %q inside this repository", c.ModuleDir, want)
	}
	if len(c.Packages) != 1 {
		t.Fatalf("checked %d packages, want tested alone", len(c.Packages))
	}
	r := &c.Packages[0].Report
	if b := r.Baseline; b == nil || b.Tokenizer != TokenizerEst || !b.TokensComparable {
		t.Errorf("baseline block = %+v, want tokenizer est, comparable", b)
	}
	want := map[string]string{
		"changed_func_cognitive_max": "tested/grade.go:8",
		"dup_blocks":                 "tested/degraded.go:15",
		"globals":                    "tested/degraded.go:9",
		"untested_exports":           "tested/degraded.go:13",
	}
	seen := 0
	for _, v := range r.Violations {
		if v.Location == nil || v.Location.File == "" || v.Location.Line == 0 {
			t.Errorf("%s has no location", v.Metric)
			continue
		}
		loc := v.Location.File + ":" + strconv.Itoa(v.Location.Line)
		if w, ok := want[v.Metric]; ok {
			seen++
			if loc != w {
				t.Errorf("%s located at %s, want %s", v.Metric, loc, w)
			}
		}
	}
	if seen != len(want) {
		t.Errorf("found %d of the %d degraded violations: %+v", seen, len(want), r.Violations)
	}
}
