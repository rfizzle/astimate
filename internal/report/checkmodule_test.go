package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// moduleCheck returns fixedCheck with a module row that broke a
// dup_blocks_cross_pkg rule against a baseline.
func moduleCheck() *Check {
	c := fixedCheck()
	two, one := 2, 1
	r := Build(&Input{Language: "go", PackagePath: metrics.ModuleRowID, ModulePath: "example.com/app",
		Metrics: metrics.RawMetrics{DupBlocksCrossPkg: &two}, Params: params()})
	ApplyGate(&r, "a1b2c3d", &metrics.RawMetrics{DupBlocksCrossPkg: &one}, &gate.Result{
		Violations: []gate.Violation{
			{Metric: "dup_blocks_cross_pkg", Base: 1, Head: 2, HasBase: true, Limit: "max_delta +0", Suggestion: "Extract them."},
		},
	})
	c.Module = &CheckedPackage{Report: r}
	return c
}

func TestCheckModuleRow(t *testing.T) {
	t.Parallel()

	t.Run("text", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		if err := WriteCheckText(&buf, moduleCheck()); err != nil {
			t.Fatal(err)
		}
		want := "violations:\n" +
			"  <module>\n" +
			"    dup_blocks_cross_pkg: 1 -> 2, max_delta +0. Extract them.\n" +
			"  internal/billing\n"
		if !strings.HasPrefix(buf.String(), want) {
			t.Errorf("text =\n%s\nwant it to start with\n%s", buf.String(), want)
		}
		summary := "\n<module>: dup_blocks_cross_pkg 2, 1 violation, 0 warnings, 0 exempted\ninternal/billing: "
		if !strings.Contains(buf.String(), summary) {
			t.Errorf("text =\n%s\nwant the module summary line before the packages'", buf.String())
		}
	})
	t.Run("new since baseline", func(t *testing.T) {
		t.Parallel()
		c := moduleCheck()
		c.Module.Report.Baseline = nil
		var buf bytes.Buffer
		if err := WriteCheckText(&buf, c); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "\n<module>: dup_blocks_cross_pkg 2, 1 violation, 0 warnings, 0 exempted, new since baseline\n") {
			t.Errorf("text =\n%s\nwant the module row marked new since baseline", buf.String())
		}
	})
	t.Run("json", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		if err := WriteCheckJSON(&buf, moduleCheck()); err != nil {
			t.Fatal(err)
		}
		var got []Report
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 || got[0].PackagePath != metrics.ModuleRowID || got[1].PackagePath != "internal/billing" {
			t.Fatalf("json has %d reports starting %+v, want the module row first of 3", len(got), got)
		}
		// The id is written as is, not HTML-escaped to \u003cmodule\u003e.
		if !strings.Contains(buf.String(), `"package_path": "<module>"`) {
			t.Errorf("json does not carry the module row's package_path unescaped:\n%s", buf.String())
		}
		if got[0].Passed == nil || *got[0].Passed || len(got[0].Violations) != 1 {
			t.Errorf("module entry = passed %v violations %+v, want failed with one", got[0].Passed, got[0].Violations)
		}
	})
	t.Run("github", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		if err := WriteGitHub(&buf, moduleCheck()); err != nil {
			t.Fatal(err)
		}
		want := "::error::module: dup_blocks_cross_pkg: 1 -> 2, max_delta +0. Extract them.\n" +
			"::error file=internal/billing::dup_blocks: 1 -> 3"
		if !strings.HasPrefix(buf.String(), want) {
			t.Errorf("github =\n%s\nwant it to start with\n%s", buf.String(), want)
		}
	})
	t.Run("github located", func(t *testing.T) {
		t.Parallel()
		c := moduleCheck()
		c.Module.Report.Violations[0].Location = &Location{File: "a/a,b.go", Line: 12}
		var buf bytes.Buffer
		if err := WriteGitHub(&buf, c); err != nil {
			t.Fatal(err)
		}
		want := "::error file=a/a%2Cb.go,line=12::module: dup_blocks_cross_pkg: 1 -> 2, max_delta +0. Extract them.\n" +
			"::error file=internal/billing::dup_blocks: 1 -> 3"
		if !strings.HasPrefix(buf.String(), want) {
			t.Errorf("github =\n%s\nwant it to start with\n%s", buf.String(), want)
		}
		// JSON carries the same location on the finding.
		var js bytes.Buffer
		if err := WriteCheckJSON(&js, c); err != nil {
			t.Fatal(err)
		}
		var got []Report
		if err := json.Unmarshal(js.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if l := got[0].Violations[0].Location; l == nil || *l != (Location{File: "a/a,b.go", Line: 12}) {
			t.Errorf("json module finding location = %+v, want a/a,b.go line 12:\n%s", l, js.String())
		}
	})
	t.Run("hook and failed", func(t *testing.T) {
		t.Parallel()
		c := passingCheck()
		c.Module = moduleCheck().Module
		if !c.Failed() {
			t.Error("Failed = false with a failing module row")
		}
		var out, warn bytes.Buffer
		if err := WriteHook(&out, &warn, c); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), `"decision":"block"`) || !strings.Contains(out.String(), `\n  <module>\n    dup_blocks_cross_pkg: 1 -> 2`) {
			t.Errorf("hook = %s, want a block naming the module row's violation", out.String())
		}
	})
}
