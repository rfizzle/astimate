package mcpserver

import (
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/report"
)

// warnConfig returns the embedded default with every top-level rule marked
// severity: warn.
func warnConfig(t *testing.T) *config.Config {
	t.Helper()
	lines := strings.Split(string(config.Default()), "\n")
	out := make([]string, 0, len(lines)+20)
	for _, l := range lines {
		out = append(out, l)
		if strings.HasPrefix(l, "  - metric: ") {
			out = append(out, "    severity: warn")
		}
	}
	cfg, err := config.Parse([]byte(strings.Join(out, "\n")))
	if err != nil {
		t.Fatalf("parsing the warn config: %v", err)
	}
	for _, th := range cfg.Thresholds {
		if !th.Warns() {
			t.Fatalf("rule %s is not a warn rule", th.Metric)
		}
	}
	return cfg
}

// TestCheckPackageWarnRules checks the degraded fixture under the default
// rules and under the same rules marked warn: the warn rules report every
// finding the fail rules report as a violation, identical but for severity
// warn, among the warnings, and the check passes, its text listing them
// under warnings.
func TestCheckPackageWarnRules(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	check := func(cfg *config.Config) (CheckResult, string) {
		ws := workspace(t, degradedDir)
		cs := newTestClient(t, Options{Config: cfg, WorkDir: ws, Version: "test"})
		res := callCheck(t, cs, map[string]any{"path": "mod/tested", "baseline_file": "baseline.json"})
		if res.IsError {
			t.Fatalf("IsError = true, want a gate result; text:\n%s", resultText(res))
		}
		return decodeCheck(t, res), resultText(res)
	}
	fail, _ := check(defaultConfig(t))
	warn, text := check(warnConfig(t))

	if fail.Passed == nil || *fail.Passed || len(fail.Violations) == 0 {
		t.Fatalf("default rules: passed %v violations %+v, want the degraded fixture to fail", fail.Passed, fail.Violations)
	}
	if warn.Passed == nil || !*warn.Passed || len(warn.Violations) != 0 {
		t.Fatalf("warn rules: passed %v violations %+v, want a pass with no violations", warn.Passed, warn.Violations)
	}
	if warn.Module == nil || warn.Module.Passed == nil || !*warn.Module.Passed {
		t.Errorf("warn rules: module block %+v, want it passed", warn.Module)
	}
	for _, v := range fail.Violations {
		want := v
		want.Severity = "warn"
		if !slices.ContainsFunc(warn.Warnings, func(w report.Finding) bool { return sameFinding(w, want) }) {
			t.Errorf("warn rules: no warning %+v among %+v", want, warn.Warnings)
		}
	}
	if !strings.HasPrefix(text, "PASSED") {
		t.Errorf("text = %q, want it to start with PASSED", text)
	}
	_, warnings, ok := strings.Cut(text, "\nwarnings:\n  tested\n")
	if !ok {
		t.Fatalf("text has no warnings section for tested:\n%s", text)
	}
	for _, m := range degradedMetrics() {
		if !strings.Contains(warnings, "    "+m+": ") {
			t.Errorf("text does not list %s under warnings:\n%s", m, text)
		}
	}
	if strings.Contains(text, "violations:") {
		t.Errorf("text has a violations section under warn rules:\n%s", text)
	}
}

// sameFinding reports whether a and b have the same fields, location
// included.
func sameFinding(a, b report.Finding) bool {
	eqBase := (a.Base == nil) == (b.Base == nil) && (a.Base == nil || *a.Base == *b.Base)
	eqLoc := (a.Location == nil) == (b.Location == nil) && (a.Location == nil || *a.Location == *b.Location)
	return a.Metric == b.Metric && a.Head == b.Head && a.Limit == b.Limit && a.Suggestion == b.Suggestion &&
		a.Severity == b.Severity && eqBase && eqLoc
}
