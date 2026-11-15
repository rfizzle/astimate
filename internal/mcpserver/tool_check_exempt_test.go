package mcpserver

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate/exempt"
)

// TestCheckPackageExemptions checks the degraded fixture under the default
// thresholds with an exemption on each of its tested package's violations:
// the call passes, and both the text and the structured content list every
// exempted finding with its reason. The dup_blocks finding carries the
// duplication_pct breach of the same paste (SPEC.md 8.1), so it is
// exempted only because both rules are. An exemption on another package
// is not reported stale by this one-package check.
func TestCheckPackageExemptions(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	cfg, err := config.Parse(pinnedDefault())
	if err != nil {
		t.Fatal(err)
	}
	violated := []string{"changed_func_cognitive_max", "dup_blocks", "duplication_pct", "globals", "untested_exports"}
	for _, m := range violated {
		cfg.Exemptions = append(cfg.Exemptions, exempt.Entry{Package: "tested", Metric: m, Reason: "fixture keeps " + m})
	}
	cfg.Exemptions = append(cfg.Exemptions, exempt.Entry{Package: "a", Metric: "globals", Reason: "not judged here"})

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
	reported := slices.DeleteFunc(slices.Clone(violated), func(m string) bool { return m == "duplication_pct" })
	if !slices.Equal(got, reported) {
		t.Errorf("exempted metrics = %q, want %q", got, reported)
	}
	for _, e := range cr.Exemptions {
		if e.Metric == "dup_blocks" && !slices.Equal(e.Also, []string{"duplication_pct"}) {
			t.Errorf("dup_blocks also = %q, want duplication_pct", e.Also)
		}
	}
	if want := "PASSED: tested is no worse than its baseline. 4 violation(s) are exempted"; !strings.HasPrefix(text, want) {
		t.Errorf("text = %q, want it to start with %q", text, want)
	}
	if !strings.Contains(text, "\ntested: 0.0 passes (ONE_PASS), 0 violations, 0 warnings, 4 exempted, +0.0 passes from baseline\n") {
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

// TestCheckPackageExemptionsFile checks that check_package honours the
// exemptions file: beside the configuration file the server resolved, read
// once with it, and under the embedded default in the checked module's
// root, read on every call, so an edit to it shows on the next call.
func TestCheckPackageExemptionsFile(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	entry := func(reason string) string {
		return "exemptions:\n  - package: tested\n    metric: globals\n    reason: " + reason + "\n"
	}
	// globalsReason returns the reason the globals finding of the check was
	// exempted with, failing when it was not exempted.
	globalsReason := func(t *testing.T, cs *mcp.ClientSession) string {
		t.Helper()
		res := callCheck(t, cs, map[string]any{"path": "mod/tested", "baseline_file": "baseline.json"})
		if res.IsError {
			t.Fatalf("IsError = true, want a gate result; text:\n%s", resultText(res))
		}
		cr := decodeCheck(t, res)
		for _, v := range cr.Violations {
			if v.Metric == "globals" {
				t.Fatalf("globals is a violation, want it exempted; text:\n%s", resultText(res))
			}
		}
		for _, e := range cr.Exemptions {
			if e.Metric == "globals" {
				return e.Reason
			}
		}
		t.Fatalf("globals is not exempted; text:\n%s", resultText(res))
		return ""
	}
	write := func(t *testing.T, dir, name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("beside the configuration file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		write(t, dir, config.FileName, string(pinnedDefault()))
		write(t, dir, config.ExemptionsFileName, entry("listed beside the config"))
		cfg, source, err := config.Resolve(filepath.Join(dir, config.FileName))
		if err != nil || source != config.SourceFlag {
			t.Fatalf("config.Resolve = %q, %v", source, err)
		}
		cs := newTestClient(t, Options{Config: cfg, WorkDir: workspace(t, degradedDir), Version: "test"})
		if got := globalsReason(t, cs); got != "listed beside the config" {
			t.Errorf("reason = %q, want the file's", got)
		}
	})
	t.Run("embedded default reads the module root on every call", func(t *testing.T) {
		t.Parallel()

		if _, err := os.Stat(config.FileName); err == nil {
			t.Fatalf("%s exists in the test's working directory", config.FileName)
		}
		cfg, source, err := config.Resolve("")
		if err != nil || source != config.SourceEmbedded {
			t.Fatalf("config.Resolve(\"\") = %q, %v; want the embedded default", source, err)
		}
		ws := workspace(t, degradedDir)
		mod := filepath.Join(ws, "mod")
		write(t, mod, config.ExemptionsFileName, entry("listed in the module root"))
		cs := newTestClient(t, Options{Config: cfg, WorkDir: ws, Version: "test"})
		if got := globalsReason(t, cs); got != "listed in the module root" {
			t.Errorf("reason = %q, want the module root file's", got)
		}
		write(t, mod, config.ExemptionsFileName, entry("edited since"))
		if got := globalsReason(t, cs); got != "edited since" {
			t.Errorf("reason after the edit = %q, want the edited file's", got)
		}
	})
}
