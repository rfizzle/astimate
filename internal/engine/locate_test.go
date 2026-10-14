package engine

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
)

// degradedDir is the degraded copy of the fixture: its tested package adds
// a duplicate function, an untested export, a global and a complex function.
const degradedDir = "../../testdata/go/fixture-degraded"

// TestCheckLocatesDegradedFindings checks the degraded fixture against a
// baseline of the pristine one: each violation of its tested package is
// located on the line the degraded copy added, module-relative, the
// duplicate block it cites is said to be added in this change and the
// untested export it added is named first, and the check carries the
// module root's directory in its repository. The copy is a code block, so
// it fails under the default's dup_blocks rule and under a dup_blocks_code
// rule in its place alike.
func TestCheckLocatesDegradedFindings(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	fx, err := LoadTarget(fixtureDir, TargetOptions{Config: pinnedConfig(t), Tokenizer: TokenizerEst})
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
	named, err := baseline.CollectNamed(t.Context(), fx.Ext, fx.Mod, pkgs)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := baseline.WriteContents(path, baseline.Contents{
		Ref: "fixture", ModulePath: fx.Mod.ModulePath, Tokenizer: TokenizerEst, Packages: pkgs, Functions: funcs, Named: named,
	}); err != nil {
		t.Fatal(err)
	}

	checkDegraded(t, path, pinnedConfig(t), "dup_blocks",
		"1 duplicate block of any kind (1 code) covers 12.2% of lines; extract shared helpers, starting with degraded.go:15-20, added in this change.")
	code, err := config.Parse(bytes.Replace(pinnedDefault(), []byte("  - metric: dup_blocks\n"), []byte("  - metric: dup_blocks_code\n"), 1))
	if err != nil {
		t.Fatal(err)
	}
	checkDegraded(t, path, code, "dup_blocks_code",
		"1 duplicate code block (of 1 of any kind; not declarative, idiom or self-overlap); extract shared helpers, starting with degraded.go:15-20, added in this change.")
}

// checkDegraded checks the degraded fixture under cfg against the
// baseline file at path, its duplication judged by the rule on dup, whose
// suggestion is dupText.
func checkDegraded(t *testing.T, path string, cfg *config.Config, dup, dupText string) {
	t.Helper()
	tg, err := LoadTarget(degradedDir, TargetOptions{Config: cfg, Tokenizer: TokenizerEst})
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
		dup:                          "tested/degraded.go:15",
		"globals":                    "tested/degraded.go:9",
		"untested_exports":           "tested/degraded.go:13",
	}
	wantText := map[string]string{
		dup:                dupText,
		"untested_exports": "no test (JoinAgain)",
	}
	seen := 0
	for _, v := range r.Violations {
		if v.Location == nil || v.Location.File == "" || v.Location.Line == 0 {
			t.Errorf("%s has no location", v.Metric)
			continue
		}
		loc := v.Location.File + ":" + strconv.Itoa(v.Location.Line)
		if s, ok := wantText[v.Metric]; ok && !strings.Contains(v.Suggestion, s) {
			t.Errorf("%s suggestion %q, want it to contain %q", v.Metric, v.Suggestion, s)
		}
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
