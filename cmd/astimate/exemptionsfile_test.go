package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/metrics"
)

// writeExemptionsBeside writes data as the exemptions file beside the
// configuration file cfg.
func writeExemptionsBeside(t *testing.T, cfg, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(filepath.Dir(cfg), config.ExemptionsFileName), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// exemptionWarnings decodes a json check and returns each row's warnings
// whose limit has prefix, as "<package> <limit>: <suggestion>".
func exemptionWarnings(t *testing.T, out, prefix string) []string {
	t.Helper()
	var reports []struct {
		PackagePath string `json:"package_path"`
		Warnings    []struct {
			Limit      string `json:"limit"`
			Suggestion string `json:"suggestion"`
		} `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &reports); err != nil {
		t.Fatalf("decoding the json check: %v\n%s", err, out)
	}
	var got []string
	for _, r := range reports {
		for _, w := range r.Warnings {
			if strings.HasPrefix(w.Limit, prefix) {
				got = append(got, r.PackagePath+" "+w.Limit+": "+w.Suggestion)
			}
		}
	}
	return got
}

// TestCheckExemptionsFile checks the degraded fixture, whose one rule
// under globalsOnlyConfig it breaks once, with the exemptions file beside
// the configuration (here in a directory of its own, as --config names it):
// its entries exempt, expire and go stale like the configuration's own,
// after them in list order, and its errors name the file.
func TestCheckExemptionsFile(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	base := fixtureBaseline(t)
	const file = config.ExemptionsFileName
	tests := []struct {
		name   string
		config string // the configuration's exemptions section
		file   string // the exemptions file
		format string
		extra  []string
		check  func(t *testing.T, code int, out, errOut string)
	}{
		{
			name: "an entry exempts the violation", file: exemptGlobals, format: formatText, extra: []string{"--all"},
			check: func(t *testing.T, code int, out, errOut string) {
				want := "\n exempted  tested  globals\n   " + degradedGlobalsReason + "\n"
				if code != exitOK || !strings.HasPrefix(out, "PASS ") || !strings.Contains(out, want) {
					t.Errorf("exit %d, want %d, a pass and %q\nstdout:\n%s\nstderr:\n%s", code, exitOK, want, out, errOut)
				}
			},
		},
		{
			name: "the configuration's entries come first", config: exemptGlobals,
			file:   "exemptions:\n  - package: tested\n    metric: globals\n    reason: the file's reason\n",
			format: formatJSON,
			check: func(t *testing.T, code int, out, errOut string) {
				if code != exitOK || !strings.Contains(out, `"reason": "`+degradedGlobalsReason+`"`) || strings.Contains(out, "the file's reason") {
					t.Errorf("exit %d, want %d with the configuration's reason, the first match\nstdout:\n%s\nstderr:\n%s",
						code, exitOK, out, errOut)
				}
			},
		},
		{
			name:   "an expired entry fails again and is reported naming the file",
			file:   "exemptions:\n  - package: tested\n    metric: globals\n    reason: once\n    expires: 2020-01-01\n",
			format: formatJSON,
			check: func(t *testing.T, code int, out, errOut string) {
				if code != exitGateFailed {
					t.Errorf("exit %d, want %d: the violation fails again\nstderr:\n%s", code, exitGateFailed, errOut)
				}
				want := "tested exemption expired 2020-01-01: The exemption for globals on tested in " + file +
					" expired on 2020-01-01 and no longer applies; fix the finding or renew the exemption with a new date. Its reason: once"
				if got := exemptionWarnings(t, out, "exemption expired"); len(got) != 1 || got[0] != want {
					t.Errorf("expired notices = %q, want [%q]", got, want)
				}
			},
		},
		{
			name: "a stale entry is reported under --all naming the file",
			file: exemptGlobals + "  - package: a\n    metric: globals\n    reason: a kept a global once\n" +
				"  - package: gone\n    metric: globals\n    reason: gone was deleted\n",
			format: formatJSON, extra: []string{"--all"},
			check: func(t *testing.T, code int, out, errOut string) {
				if code != exitOK {
					t.Errorf("exit %d, want %d: stale exemptions only warn\nstderr:\n%s", code, exitOK, errOut)
				}
				got := strings.Join(exemptionWarnings(t, out, "stale exemption"), "\n")
				for _, want := range []string{
					"a stale exemption: The exemption for globals on a in " + file + " matched no violation; remove it from " + file + ".",
					metrics.ModuleRowID + " stale exemption: The exemption for globals on gone in " + file +
						" matched no violation: the module has no such package. Remove it from " + file + ".",
				} {
					if !strings.Contains(got, want) {
						t.Errorf("stale notices lack %q:\n%s", want, got)
					}
				}
			},
		},
		{
			name: "an unknown top-level key is a configuration error naming file and key",
			file: exemptGlobals + "thresholds: []\n", format: formatText,
			check: func(t *testing.T, code int, out, errOut string) {
				if code != exitAnalysis || !strings.Contains(errOut, file+": thresholds: the file holds only exemptions") {
					t.Errorf("exit %d, want %d naming %s and thresholds\nstdout:\n%s\nstderr:\n%s", code, exitAnalysis, file, out, errOut)
				}
			},
		},
		{
			name: "an invalid entry names the file and its index",
			file: exemptGlobals + "  - package: tested\n    metric: globals\n", format: formatText,
			check: func(t *testing.T, code int, out, errOut string) {
				if code != exitAnalysis || !strings.Contains(errOut, file+": exemptions[1]: reason is required") {
					t.Errorf("exit %d, want %d naming %s: exemptions[1]\nstdout:\n%s\nstderr:\n%s", code, exitAnalysis, file, out, errOut)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := globalsOnlyConfig(t, tt.config)
			writeExemptionsBeside(t, cfg, tt.file)
			code, out, errOut := runExempt(t, base, cfg, tt.format, tt.extra...)
			tt.check(t, code, out, errOut)
		})
	}
}

// configOnlyExemptions exempts one of the degraded fixture's violations,
// carries one expired and one stale exemption, all in the configuration.
const configOnlyExemptions = "\nexemptions:\n" +
	"  - package: tested\n    metric: globals\n    reason: " + degradedGlobalsReason + "\n" +
	"  - package: tested\n    metric: untested_exports\n    reason: expired once\n    expires: 2020-01-01\n" +
	"  - package: a\n    metric: globals\n    reason: a kept a global once\n"

// TestHookGoldenConfigExemptions checks the hook's reason on the degraded
// fixture under exemptions in the configuration alone (an exempted
// violation, an expired and a stale exemption) against
// testdata/check_degraded_hook_exempted.txt, which holds the reason as it
// was before the exemptions file existed, with and without an empty
// exemptions file beside the configuration. UPDATE_GOLDEN=1 regenerates it.
func TestHookGoldenConfigExemptions(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	base := writeHookBaseline(t)
	for _, tt := range []struct {
		name string
		file *string
	}{
		{name: "no exemptions file"},
		{name: "empty exemptions file", file: new("exemptions: []\n")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := filepath.Join(t.TempDir(), "pinned.yaml")
			if err := os.WriteFile(cfg, append(pinnedDefault(), configOnlyExemptions...), 0o644); err != nil {
				t.Fatal(err)
			}
			if tt.file != nil {
				writeExemptionsBeside(t, cfg, *tt.file)
			}
			var out, errOut bytes.Buffer
			args := []string{"check", "--all", "--baseline", base, "--config", cfg, "--format", "hook", hookTestdata("fixture-degraded")}
			if got := run(args, &out, &errOut); got != exitOK {
				t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, errOut.String())
			}
			var block struct{ Reason string }
			if err := json.Unmarshal(out.Bytes(), &block); err != nil || block.Reason == "" {
				t.Fatalf("hook stdout = %q, want a block with a reason (%v)", out.String(), err)
			}
			if tt.file != nil && os.Getenv("UPDATE_GOLDEN") != "" {
				return // the case without the file writes the golden
			}
			compareGolden(t, "check_degraded_hook_exempted.txt", block.Reason)
		})
	}
}
