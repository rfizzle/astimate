package report

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// updateFlag names the flag that makes TestSeverityFormats rewrite its
// golden files under testdata/severity. Use it only after an intentional
// change to a format, then read the result.
const updateFlag = "update"

// TestMain defines the -update flag before the test binary parses flags;
// the golden test reads it back with flag.Lookup, so no package variable
// holds it.
func TestMain(m *testing.M) {
	flag.Bool(updateFlag, false, "rewrite the report golden files")
	flag.Parse()
	os.Exit(m.Run())
}

// severityCheck returns a check of one package judged by the globals
// ratchet at severity sev, which the package breaks by one new global,
// beside a capacity rule it approaches; the finding is located on the
// global's declaration as the engine would locate it.
func severityCheck(sev gate.Severity) *Check {
	globals := gate.Threshold{Metric: "globals", Kind: gate.Density, MaxDelta: new(0.0), RatchetFromZero: true, Severity: sev}
	tokens := gate.Threshold{Metric: "tokens_est", Kind: gate.Capacity, Max: new(16000.0), WarnAt: 0.75, Severity: sev}
	head := metrics.RawMetrics{Globals: 1, TokensEst: 13000}
	base := metrics.RawMetrics{TokensEst: 12500}
	suggest := func(metric string, _ float64, _ metrics.RawMetrics) string {
		if metric == "globals" {
			return "Pass registry explicitly instead of a package variable."
		}
		return ""
	}
	res := gate.Evaluate(head, &base, []gate.Threshold{globals, tokens}, suggest)
	r := Build(&Input{Language: "go", PackagePath: "internal/registry", ModulePath: "example.com/app",
		Metrics: head, Params: params(), ConfigVersion: uncalibratedVersion})
	ApplyGate(&r, "a1b2c3d", &base, &res)
	for _, f := range r.AllFindings() {
		if f.Metric == "globals" {
			f.Location = &Location{File: "internal/registry/registry.go", Line: 9}
		}
	}
	passes := 0.0
	return &Check{Packages: []CheckedPackage{{Report: r, BaseAgentPasses: &passes}}}
}

// TestSeverityFormats renders the same breach under a fail rule and a warn
// rule in every check format and compares each with its golden file: the
// fail rule's violation fails the check, and the warn rule reports the
// identical finding as a warning, with severity warn in JSON, without
// failing it.
func TestSeverityFormats(t *testing.T) {
	t.Parallel()

	formats := []struct {
		name  string
		write func(*Check) (string, error)
	}{
		{"text", func(c *Check) (string, error) {
			var b bytes.Buffer
			err := WriteCheckText(&b, c)
			return b.String(), err
		}},
		{"json", func(c *Check) (string, error) {
			var b bytes.Buffer
			err := WriteCheckJSON(&b, c)
			return b.String(), err
		}},
		{"hook", func(c *Check) (string, error) {
			var out, warn bytes.Buffer
			err := WriteHook(&out, &warn, c)
			return "stdout:\n" + out.String() + "stderr:\n" + warn.String(), err
		}},
		{"github", func(c *Check) (string, error) {
			var b bytes.Buffer
			err := WriteGitHub(&b, c)
			return b.String(), err
		}},
	}
	for _, f := range formats {
		for _, sev := range []gate.Severity{gate.SeverityFail, gate.SeverityWarn} {
			name := f.name + "-" + string(sev)
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				c := severityCheck(sev)
				if c.Failed() != (sev == gate.SeverityFail) {
					t.Errorf("Failed() = %v under severity %s", c.Failed(), sev)
				}
				got, err := f.write(c)
				if err != nil {
					t.Fatal(err)
				}
				golden := filepath.Join("testdata", "severity", name+".golden")
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
					t.Errorf("%s output differs from %s; rerun with -%s and read the diff:\n%s", name, golden, updateFlag, got)
				}
			})
		}
	}
}

// TestSeverityIdenticalFinding checks that the warn rule's warning is the
// fail rule's violation, field for field, plus severity warn, and that a
// capacity warning, which was never a violation, carries no severity.
func TestSeverityIdenticalFinding(t *testing.T) {
	t.Parallel()

	fail, warn := severityCheck(gate.SeverityFail).Packages[0].Report, severityCheck(gate.SeverityWarn).Packages[0].Report
	if len(fail.Violations) != 1 || len(warn.Violations) != 0 || len(warn.Warnings) != 2 {
		t.Fatalf("fail violations %+v, warn violations %+v warnings %+v", fail.Violations, warn.Violations, warn.Warnings)
	}
	want := fail.Violations[0]
	want.Severity = "warn"
	got := warn.Warnings[0]
	if g, w := mustJSON(t, got), mustJSON(t, want); g != w {
		t.Errorf("warning = %s, want %s", g, w)
	}
	if !strings.Contains(mustJSON(t, got), `"severity":"warn"`) {
		t.Errorf("warning JSON %s lacks severity warn", mustJSON(t, got))
	}
	if strings.Contains(mustJSON(t, fail.Violations[0]), "severity") || strings.Contains(mustJSON(t, warn.Warnings[1]), "severity") {
		t.Errorf("severity set on a finding that is not a warn rule's breach: %s, %s", mustJSON(t, fail.Violations[0]), mustJSON(t, warn.Warnings[1]))
	}
}

// mustJSON encodes v compactly.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
