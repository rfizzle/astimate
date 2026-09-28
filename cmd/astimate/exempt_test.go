package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/metrics"
)

// degradedGlobalsReason is the reason of the exemption the end-to-end tests
// put on the degraded fixture's globals violation.
const degradedGlobalsReason = "joins is a fixture global kept on purpose"

// globalsOnlyConfig writes the embedded default with its thresholds
// replaced by the globals ratchet alone, so the degraded fixture breaks one
// rule, followed by exemptions, and returns its path. The languages
// section, which follows the thresholds, is dropped with them.
func globalsOnlyConfig(t *testing.T, exemptions string) string {
	t.Helper()
	head, _, ok := strings.Cut(string(config.Default()), "\nthresholds:\n")
	if !ok {
		t.Fatal("the embedded default has no thresholds section")
	}
	data := head + "\nthresholds:\n" +
		"  - metric: globals\n    kind: density\n    max_delta: 0\n    ratchet_from_zero: true\n" +
		exemptions
	p := filepath.Join(t.TempDir(), "astimate.yaml")
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// exemptGlobals is an exemptions section silencing the degraded fixture's
// globals violation.
const exemptGlobals = "exemptions:\n" +
	"  - package: tested\n    metric: globals\n    reason: " + degradedGlobalsReason + "\n    expires: 2999-12-31\n"

// runExempt runs check on the degraded fixture against base with config
// cfg in format and returns the exit code, stdout and stderr.
func runExempt(t *testing.T, base, cfg, format string, extra ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	args := append([]string{"check", degradedDir, "--baseline", base, "--config", cfg, "--format", format}, extra...)
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// TestCheckExemptionFixture checks the degraded fixture under a config
// whose one rule it breaks once: failing without an exemption, and passing
// with one that matches, the reason shown in every format.
func TestCheckExemptionFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	base := fixtureBaseline(t)
	exempt := globalsOnlyConfig(t, exemptGlobals)

	t.Run("without the exemption it fails", func(t *testing.T) {
		t.Parallel()

		code, out, errOut := runExempt(t, base, globalsOnlyConfig(t, ""), formatText, "--all")
		if code != exitGateFailed || !strings.Contains(out, "\ntested: 1 violation, 0 warnings, 0 exempted\n") {
			t.Errorf("exit %d, want %d with one violation on tested\nstdout:\n%s\nstderr:\n%s", code, exitGateFailed, out, errOut)
		}
	})
	t.Run("text", func(t *testing.T) {
		t.Parallel()

		code, out, errOut := runExempt(t, base, exempt, formatText, "--all")
		if code != exitOK {
			t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, out, errOut)
		}
		want := "exempted:\n  tested\n    globals: 0 -> 1, max_delta +0. "
		if !strings.HasPrefix(out, want) {
			t.Errorf("text does not open with the exempted section %q:\n%s", want, out)
		}
		if !strings.Contains(out, " Exempted: "+degradedGlobalsReason+"\n") {
			t.Errorf("text does not print the reason:\n%s", out)
		}
		if !strings.Contains(out, "\ntested: 0 violations, 0 warnings, 1 exempted\n") {
			t.Errorf("text has no summary line counting the exemption:\n%s", out)
		}
	})
	t.Run("json", func(t *testing.T) {
		t.Parallel()

		code, out, errOut := runExempt(t, base, exempt, formatJSON, "--all")
		if code != exitOK {
			t.Fatalf("exit %d, want %d\nstderr:\n%s", code, exitOK, errOut)
		}
		for _, r := range decodeReports(t, []byte(out)) {
			if r.Exemptions == nil {
				t.Errorf("%s: exemptions absent, want an array whenever a gate ran", r.PackagePath)
			}
			if r.PackagePath != "tested" {
				if len(r.Exemptions) != 0 {
					t.Errorf("%s: exemptions = %+v, want none", r.PackagePath, r.Exemptions)
				}
				continue
			}
			if len(r.Violations) != 0 || r.Passed == nil || !*r.Passed || len(r.Exemptions) != 1 {
				t.Fatalf("tested = violations %+v exemptions %+v passed %v, want globals exempted and a pass",
					r.Violations, r.Exemptions, r.Passed)
			}
			e := r.Exemptions[0]
			if e.Metric != "globals" || e.Reason != degradedGlobalsReason || e.Base == nil || *e.Base != 0 || e.Head != 1 ||
				e.Limit != "max_delta +0" || !strings.Contains(e.Suggestion, degradedGlobal) {
				t.Errorf("exempted finding = %+v, want the globals violation with its reason", e)
			}
			if e.Location == nil || e.Location.File != "tested/degraded.go" || e.Location.Line != 9 {
				t.Errorf("exempted location = %+v, want tested/degraded.go:9", e.Location)
			}
		}
	})
	t.Run("hook", func(t *testing.T) {
		t.Parallel()

		code, out, errOut := runExempt(t, base, exempt, formatHook, "--all")
		if code != exitOK || out != "{}\n" {
			t.Errorf("exit %d stdout %q, want %d and {}", code, out, exitOK)
		}
		if !strings.Contains(errOut, "exempted:\n  tested\n    globals: ") || !strings.Contains(errOut, degradedGlobalsReason) {
			t.Errorf("stderr does not list the exempted finding with its reason:\n%s", errOut)
		}
	})
	t.Run("github", func(t *testing.T) {
		t.Parallel()

		code, out, errOut := runExempt(t, base, exempt, formatGitHub, "--all")
		if code != exitOK {
			t.Fatalf("exit %d, want %d\nstderr:\n%s", code, exitOK, errOut)
		}
		abs, err := filepath.Abs(degradedDir)
		if err != nil {
			t.Fatal(err)
		}
		file := path.Join(baseline.RepoDir(t.Context(), abs), "tested", "degraded.go")
		want := "::notice file=" + file + ",line=9::globals: 0 -> 1, max_delta +0. "
		if !strings.HasPrefix(out, want) || !strings.Contains(out, " Exempted: "+degradedGlobalsReason+"\n") {
			t.Errorf("github = %q, want a notice starting %q and ending with the reason", out, want)
		}
		if strings.Contains(out, "::error") {
			t.Errorf("github has an error annotation for an exempted finding:\n%s", out)
		}
	})
}

// TestCheckExemptionConfigErrors checks that an exemption without a reason
// fails config loading, naming its index, before any analysis.
func TestCheckExemptionConfigErrors(t *testing.T) {
	t.Parallel()

	cfg := globalsOnlyConfig(t, exemptGlobals+"  - package: tested\n    metric: globals\n")
	var out, errOut bytes.Buffer
	code := run([]string{"check", degradedDir, "--all", "--baseline", "unused.json", "--config", cfg}, &out, &errOut)
	if code != exitAnalysis || !strings.Contains(errOut.String(), "exemptions[1]: reason is required") {
		t.Errorf("exit %d, want %d naming exemptions[1]\nstdout:\n%s\nstderr:\n%s", code, exitAnalysis, out.String(), errOut.String())
	}
}

// TestCheckExemptionStale checks that an --all check reports an exemption
// that matched nothing as a warning on its package, or on the module row
// for a package the module does not have, and that a check of the changed
// packages alone does not.
func TestCheckExemptionStale(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	base := fixtureBaseline(t)
	cfg := globalsOnlyConfig(t, exemptGlobals+
		"  - package: a\n    metric: globals\n    reason: a kept a global once\n"+
		"  - package: gone\n    metric: globals\n    reason: gone was deleted\n")

	code, out, errOut := runExempt(t, base, cfg, formatText, "--all")
	if code != exitOK {
		t.Fatalf("exit %d, want %d: stale exemptions only warn\nstdout:\n%s\nstderr:\n%s", code, exitOK, out, errOut)
	}
	for _, want := range []string{
		"warnings:\n  " + metrics.ModuleRowID + "\n    globals: 0 -> 0, stale exemption. The exemption for globals on gone matched no violation: the module has no such package.",
		"\n  a\n    globals: 0 -> 0, stale exemption. The exemption for globals on a matched no violation; remove it from the config. Its reason: a kept a global once\n",
		"\na: 0 violations, 1 warning, 0 exempted\n",
		"\ntested: 0 violations, 0 warnings, 1 exempted\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text does not contain %q:\n%s", want, out)
		}
	}

	// The same check without --all compares against a baseline file whose
	// ref, "fixture", does not resolve, so it checks every package, but it
	// was not asked to judge every row and reports nothing stale.
	code, out, errOut = runExempt(t, base, cfg, formatJSON)
	if code != exitOK {
		t.Fatalf("exit %d, want %d\nstderr:\n%s", code, exitOK, errOut)
	}
	var reports []struct {
		PackagePath string `json:"package_path"`
		Warnings    []struct {
			Limit string `json:"limit"`
		} `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &reports); err != nil {
		t.Fatal(err)
	}
	for _, r := range reports {
		for _, w := range r.Warnings {
			if w.Limit == "stale exemption" {
				t.Errorf("%s: stale exemption reported without --all", r.PackagePath)
			}
		}
	}
}

// TestCheckExemptionsEmptyArray checks that every report of a check under
// the embedded default, which has no exemptions, carries an empty
// exemptions array.
func TestCheckExemptionsEmptyArray(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	var out, errOut bytes.Buffer
	args := []string{"check", fixtureDir, "--all", "--baseline", fixtureBaseline(t), "--format", formatJSON}
	if code := run(args, &out, &errOut); code != exitOK {
		t.Fatalf("exit %d, want %d\nstderr:\n%s", code, exitOK, errOut.String())
	}
	var reports []map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &reports); err != nil {
		t.Fatal(err)
	}
	if len(reports) == 0 {
		t.Fatal("no reports")
	}
	for _, r := range reports {
		if got, ok := r["exemptions"]; !ok || string(got) != "[]" {
			t.Errorf("%s: exemptions = %s (present %v), want []", r["package_path"], got, ok)
		}
	}
}
