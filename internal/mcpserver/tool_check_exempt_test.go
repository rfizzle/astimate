package mcpserver

import (
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
)

// TestCheckPackageExemptions checks the degraded fixture under the default
// thresholds with an exemption on each of its tested package's violations:
// the call passes, and both the text and the structured content list every
// exempted finding with its reason. An exemption on another package is not
// reported stale by this one-package check.
func TestCheckPackageExemptions(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	violated := []string{"changed_func_cognitive_max", "dup_blocks", "duplication_pct", "globals", "untested_exports"}
	for _, m := range violated {
		cfg.Exemptions = append(cfg.Exemptions, gate.Exemption{Package: "tested", Metric: m, Reason: "fixture keeps " + m})
	}
	cfg.Exemptions = append(cfg.Exemptions, gate.Exemption{Package: "a", Metric: "globals", Reason: "not judged here"})

	cs := newTestClient(t, Options{Config: cfg, WorkDir: workspace(t, degradedDir), Version: "test"})
	res := callCheck(t, cs, map[string]any{"path": "mod/tested", "baseline_file": "baseline.json"})
	text := resultText(res)
	if res.IsError {
		t.Fatalf("IsError = true, want a gate result; text:\n%s", text)
	}
	cr := decodeCheck(t, res)
	if cr.Passed == nil || !*cr.Passed || len(cr.Violations) != 0 {
		t.Fatalf("passed %v violations %+v, want a pass with every violation exempted; text:\n%s", cr.Passed, cr.Violations, text)
	}
	got := make([]string, 0, len(cr.Exemptions))
	for _, e := range cr.Exemptions {
		got = append(got, e.Metric)
		if e.Reason != "fixture keeps "+e.Metric {
			t.Errorf("%s: reason %q, want the exemption's", e.Metric, e.Reason)
		}
		if !strings.Contains(text, " Exempted: fixture keeps "+e.Metric+"\n") {
			t.Errorf("text does not list %s with its reason:\n%s", e.Metric, text)
		}
	}
	if !slices.Equal(got, violated) {
		t.Errorf("exempted metrics = %q, want %q", got, violated)
	}
	if want := "PASSED: tested is no worse than its baseline. 5 violation(s) are exempted"; !strings.HasPrefix(text, want) {
		t.Errorf("text = %q, want it to start with %q", text, want)
	}
	if !strings.Contains(text, "\ntested: 0.0 passes (ONE_PASS), 0 violations, 0 warnings, 5 exempted, +0.0 passes from baseline\n") {
		t.Errorf("text has no summary line counting the exemptions:\n%s", text)
	}
	if cr.Module == nil || cr.Module.Exemptions == nil {
		t.Errorf("module block = %+v, want an exemptions array", cr.Module)
	}
	for _, w := range append(slices.Clone(cr.Warnings), cr.Module.Warnings...) {
		if w.Limit == "stale exemption" {
			t.Errorf("one-package check reported a stale exemption: %s", w.Suggestion)
		}
	}
}
