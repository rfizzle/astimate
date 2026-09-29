package main

import (
	"encoding/json"
	"maps"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
)

// globalsSeverityConfig is globalsOnlyConfig with the globals ratchet at
// severity sev, followed by exemptions.
func globalsSeverityConfig(t *testing.T, sev, exemptions string) string {
	t.Helper()
	return globalsOnlyConfig(t, "    severity: "+sev+"\n"+exemptions)
}

// TestCheckSeverityFixture checks the degraded fixture's one globals breach
// under the globals ratchet at severity fail and warn, in every format: a
// violation with exit code 3 (0 with a block decision for the hook) under
// fail, and under warn the identical finding as a warning, with severity
// warn in JSON, and exit code 0.
func TestCheckSeverityFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	base := fixtureBaseline(t)
	fail := globalsSeverityConfig(t, "fail", "")
	warn := globalsSeverityConfig(t, "warn", "")
	abs, err := filepath.Abs(degradedDir)
	if err != nil {
		t.Fatal(err)
	}
	file := path.Join(baseline.RepoDir(t.Context(), abs), "tested", "degraded.go")
	const line = "globals: 0 -> 1, max_delta +0. "

	t.Run("text", func(t *testing.T) {
		t.Parallel()

		code, out, errOut := runExempt(t, base, fail, formatText, "--all")
		if code != exitGateFailed || !strings.HasPrefix(out, "violations:\n  tested\n    "+line) {
			t.Errorf("fail: exit %d, want %d with the globals violation\nstdout:\n%s\nstderr:\n%s", code, exitGateFailed, out, errOut)
		}
		violation, _, _ := strings.Cut(strings.TrimPrefix(out, "violations:\n  tested\n    "), "\n")
		code, out, errOut = runExempt(t, base, warn, formatText, "--all")
		if code != exitOK || !strings.HasPrefix(out, "warnings:\n  tested\n    "+violation+"\n") {
			t.Errorf("warn: exit %d, want %d with the warning %q\nstdout:\n%s\nstderr:\n%s", code, exitOK, violation, out, errOut)
		}
		if !strings.Contains(out, "\ntested: 0.0 passes (ONE_PASS), 0 violations, 1 warning, 0 exempted, +0.0 passes from baseline\n") {
			t.Errorf("warn: no summary line counting the warning:\n%s", out)
		}
	})
	t.Run("json", func(t *testing.T) {
		t.Parallel()

		code, out, errOut := runExempt(t, base, fail, formatJSON, "--all")
		if code != exitGateFailed {
			t.Fatalf("fail: exit %d, want %d\nstderr:\n%s", code, exitGateFailed, errOut)
		}
		failed := testedReport(t, out)
		code, out, errOut = runExempt(t, base, warn, formatJSON, "--all")
		if code != exitOK {
			t.Fatalf("warn: exit %d, want %d\nstderr:\n%s", code, exitOK, errOut)
		}
		warned := testedReport(t, out)
		if len(failed.Violations) != 1 || len(warned.Violations) != 0 || len(warned.Warnings) != 1 {
			t.Fatalf("fail violations %v, warn violations %v warnings %v, want one finding moved",
				failed.Violations, warned.Violations, warned.Warnings)
		}
		if _, ok := failed.Violations[0]["severity"]; ok {
			t.Errorf("violation %v carries a severity", failed.Violations[0])
		}
		want := maps.Clone(failed.Violations[0])
		want["severity"] = "warn"
		if got := warned.Warnings[0]; !reflect.DeepEqual(got, want) {
			t.Errorf("warning = %v, want the violation with severity warn, %v", got, want)
		}
		if failed.Passed == nil || *failed.Passed || warned.Passed == nil || !*warned.Passed {
			t.Errorf("passed = %v under fail and %v under warn, want false and true", failed.Passed, warned.Passed)
		}
	})
	t.Run("hook", func(t *testing.T) {
		t.Parallel()

		code, out, errOut := runExempt(t, base, fail, formatHook, "--all")
		if code != exitOK || !strings.HasPrefix(out, `{"decision":"block","reason":"violations:\n  tested\n    `+line) {
			t.Errorf("fail: exit %d stdout %q stderr %q, want %d and a block on the violation", code, out, errOut, exitOK)
		}
		code, out, errOut = runExempt(t, base, warn, formatHook, "--all")
		if code != exitOK || out != "{}\n" {
			t.Errorf("warn: exit %d stdout %q, want %d and {}", code, out, exitOK)
		}
		if !strings.HasPrefix(errOut, "warnings:\n  tested\n    "+line) {
			t.Errorf("warn: stderr does not list the warning:\n%s", errOut)
		}
	})
	t.Run("github", func(t *testing.T) {
		t.Parallel()

		code, out, errOut := runExempt(t, base, fail, formatGitHub, "--all")
		prefix := " file=" + file + ",line=9::" + line
		if code != exitGateFailed || !strings.HasPrefix(out, "::error"+prefix) {
			t.Errorf("fail: exit %d, want %d with an error annotation %q:\n%s\nstderr:\n%s", code, exitGateFailed, prefix, out, errOut)
		}
		failLine := strings.TrimPrefix(out, "::error")
		code, out, errOut = runExempt(t, base, warn, formatGitHub, "--all")
		if code != exitOK || out != "::warning"+failLine {
			t.Errorf("warn: exit %d, want %d with the same annotation as a warning:\n%s\nstderr:\n%s", code, exitOK, out, errOut)
		}
	})
	t.Run("exempted", func(t *testing.T) {
		t.Parallel()

		code, out, errOut := runExempt(t, base, globalsSeverityConfig(t, "warn", exemptGlobals), formatJSON, "--all")
		if code != exitOK {
			t.Fatalf("exit %d, want %d\nstderr:\n%s", code, exitOK, errOut)
		}
		r := testedReport(t, out)
		if len(r.Warnings) != 0 || len(r.Exemptions) != 1 ||
			r.Exemptions[0]["severity"] != "warn" || r.Exemptions[0]["reason"] != degradedGlobalsReason {
			t.Errorf("warnings %v exemptions %v, want the warning exempted with its severity and reason", r.Warnings, r.Exemptions)
		}
	})
}

// findingsReport is a check report with each finding decoded as a map, so
// a test can compare every field, including ones it does not name.
type findingsReport struct {
	PackagePath string           `json:"package_path"`
	Violations  []map[string]any `json:"violations"`
	Warnings    []map[string]any `json:"warnings"`
	Exemptions  []map[string]any `json:"exemptions"`
	Passed      *bool            `json:"passed"`
}

// testedReport returns the tested package's report from the JSON check
// output out.
func testedReport(t *testing.T, out string) findingsReport {
	t.Helper()
	var reports []findingsReport
	if err := json.Unmarshal([]byte(out), &reports); err != nil {
		t.Fatalf("decoding %s: %v", out, err)
	}
	for _, r := range reports {
		if r.PackagePath == "tested" {
			return r
		}
	}
	t.Fatalf("no report for tested in %s", out)
	return findingsReport{}
}
