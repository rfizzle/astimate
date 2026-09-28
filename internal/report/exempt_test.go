package report

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// exemptedCheck returns a check whose module row and one package each had
// a violation silenced by an exemption. With failing set, the package keeps
// a second violation that no exemption matched.
func exemptedCheck(failing bool) *Check {
	zero, one := 0, 1
	mod := Build(&Input{Language: "go", PackagePath: metrics.ModuleRowID, ModulePath: "example.com/app",
		Metrics: metrics.RawMetrics{DupBlocksCrossPkg: &one}, Params: params(), ConfigVersion: uncalibratedVersion})
	ApplyGate(&mod, "a1b2c3d", &metrics.RawMetrics{DupBlocksCrossPkg: &zero}, &gate.Result{
		Passed: true,
		Exempted: []gate.Exempted{{
			Violation: gate.Violation{Metric: "dup_blocks_cross_pkg", Head: 1, HasBase: true, Limit: "max_delta +0", Suggestion: "Share it."},
			Reason:    "generated clients, 50% alike",
		}},
	})
	mod.Exemptions[0].Location = &Location{File: "gen/a/client.go", Line: 3}

	res := &gate.Result{
		Passed: !failing,
		Exempted: []gate.Exempted{{
			Violation: gate.Violation{Metric: "globals", Head: 1, HasBase: true, Limit: "max_delta +0", Suggestion: "Pass it explicitly."},
			Reason:    "process-wide registry, see ADR 12",
		}},
	}
	if failing {
		res.Violations = []gate.Violation{{Metric: "dup_blocks", Base: 1, Head: 2, HasBase: true, Limit: "max_delta +0", Suggestion: "Extract helpers."}}
	}
	pkg := Build(&Input{Language: "go", PackagePath: "internal/registry", ModulePath: "example.com/app",
		Metrics: metrics.RawMetrics{DupBlocks: 2, Globals: 1}, Params: params(), ConfigVersion: uncalibratedVersion})
	ApplyGate(&pkg, "a1b2c3d", &metrics.RawMetrics{DupBlocks: 1}, res)
	pkg.Exemptions[0].Location = &Location{File: "internal/registry/registry.go", Line: 9}
	passes := 0.0
	return &Check{Module: &CheckedPackage{Report: mod}, Packages: []CheckedPackage{{Report: pkg, BaseAgentPasses: &passes}}}
}

// exemptedFindings is the findings text of exemptedCheck(true): the
// exempted section follows the others and every line carries its reason.
const exemptedFindings = "violations:\n" +
	"  internal/registry\n" +
	"    dup_blocks: 1 -> 2, max_delta +0. Extract helpers.\n" +
	"exempted:\n" +
	"  <module>\n" +
	"    dup_blocks_cross_pkg: 0 -> 1, max_delta +0. Share it. Exempted: generated clients, 50% alike\n" +
	"  internal/registry\n" +
	"    globals: 0 -> 1, max_delta +0. Pass it explicitly. Exempted: process-wide registry, see ADR 12\n"

func TestWriteCheckTextExempted(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WriteCheckText(&buf, exemptedCheck(true)); err != nil {
		t.Fatal(err)
	}
	want := exemptedFindings +
		"<module>: dup_blocks_cross_pkg 1, 0 violations, 0 warnings, 1 exempted\n" +
		"internal/registry: 1 violation, 0 warnings, 1 exempted\n"
	if buf.String() != want {
		t.Errorf("text =\n%s\nwant\n%s", buf.String(), want)
	}
}

func TestWriteCheckJSONExempted(t *testing.T) {
	t.Parallel()

	c := exemptedCheck(false)
	if c.Failed() {
		t.Fatal("a check whose only violations are exempted failed")
	}
	var buf bytes.Buffer
	if err := WriteCheckJSON(&buf, c); err != nil {
		t.Fatal(err)
	}
	var got []map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decoding %s: %v", buf.String(), err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d reports, want 2", len(got))
	}
	want := `[
      {
        "metric": "globals",
        "base": 0,
        "head": 1,
        "limit": "max_delta +0",
        "suggestion": "Pass it explicitly.",
        "location": {
          "file": "internal/registry/registry.go",
          "line": 9
        },
        "reason": "process-wide registry, see ADR 12"
      }
    ]`
	if string(got[1]["exemptions"]) != want {
		t.Errorf("exemptions =\n%s\nwant\n%s", got[1]["exemptions"], want)
	}
	if string(got[1]["violations"]) != "[]" || string(got[1]["passed"]) != "true" {
		t.Errorf("violations %s passed %s, want [] and true", got[1]["violations"], got[1]["passed"])
	}

	// A gate that exempted nothing still writes the array, empty.
	buf.Reset()
	if err := WriteCheckJSON(&buf, passingCheck()); err != nil {
		t.Fatal(err)
	}
	got = nil
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if e, ok := got[0]["exemptions"]; !ok || string(e) != "[]" {
		t.Errorf("exemptions = %s (present %v), want []", e, ok)
	}
	// A report no gate ran on, such as assess's, has none.
	r := Build(&Input{Language: "go", PackagePath: ".", ModulePath: "example.com/app", Params: params()})
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`"exemptions"`)) {
		t.Errorf("an ungated report carries exemptions: %s", data)
	}
}

func TestWriteHookExempted(t *testing.T) {
	t.Parallel()

	t.Run("failure lists exempted findings with reasons in the reason", func(t *testing.T) {
		t.Parallel()

		var out, warn bytes.Buffer
		if err := WriteHook(&out, &warn, exemptedCheck(true)); err != nil {
			t.Fatal(err)
		}
		var got hookBlock
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("decoding %q: %v", out.String(), err)
		}
		if got.Decision != "block" || got.Reason != exemptedFindings {
			t.Errorf("decision %q reason =\n%s\nwant block and\n%s", got.Decision, got.Reason, exemptedFindings)
		}
		if warn.Len() != 0 {
			t.Errorf("warnings writer = %q, want empty on failure", warn.String())
		}
	})
	t.Run("success writes them to the warnings writer", func(t *testing.T) {
		t.Parallel()

		var out, warn bytes.Buffer
		if err := WriteHook(&out, &warn, exemptedCheck(false)); err != nil {
			t.Fatal(err)
		}
		want := "exempted:\n" +
			"  <module>\n" +
			"    dup_blocks_cross_pkg: 0 -> 1, max_delta +0. Share it. Exempted: generated clients, 50% alike\n" +
			"  internal/registry\n" +
			"    globals: 0 -> 1, max_delta +0. Pass it explicitly. Exempted: process-wide registry, see ADR 12\n"
		if out.String() != "{}\n" || warn.String() != want {
			t.Errorf("stdout %q, warnings =\n%s\nwant {} and\n%s", out.String(), warn.String(), want)
		}
	})
}

func TestWriteGitHubExempted(t *testing.T) {
	t.Parallel()

	c := exemptedCheck(true)
	c.ModuleDir = "svc"
	var buf bytes.Buffer
	if err := WriteGitHub(&buf, c); err != nil {
		t.Fatal(err)
	}
	want := "::notice file=svc/gen/a/client.go,line=3::module: dup_blocks_cross_pkg: 0 -> 1, max_delta +0. Share it. Exempted: generated clients, 50%25 alike\n" +
		"::error file=svc/internal/registry::dup_blocks: 1 -> 2, max_delta +0. Extract helpers.\n" +
		"::notice file=svc/internal/registry/registry.go,line=9::globals: 0 -> 1, max_delta +0. Pass it explicitly. Exempted: process-wide registry, see ADR 12\n"
	if buf.String() != want {
		t.Errorf("github =\n%s\nwant\n%s", buf.String(), want)
	}
}

func TestAllFindings(t *testing.T) {
	t.Parallel()

	r := exemptedCheck(true).Packages[0].Report
	r.Warnings = []Finding{{Metric: "tokens_est"}}
	var got []string
	for _, f := range r.AllFindings() {
		got = append(got, f.Metric)
		f.Limit = "set"
	}
	if want := []string{"dup_blocks", "tokens_est", "globals"}; len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("AllFindings metrics = %q, want %q", got, want)
	}
	if r.Violations[0].Limit != "set" || r.Warnings[0].Limit != "set" || r.Exemptions[0].Limit != "set" {
		t.Error("AllFindings did not return pointers into the report")
	}
}
