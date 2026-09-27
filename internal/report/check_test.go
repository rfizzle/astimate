package report

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// fixedCheck returns a check with one failing package, one package with a
// warning only, and one deleted directory.
func fixedCheck() *Check {
	base := metrics.RawMetrics{DupBlocks: 1}
	failing := Build(&Input{Language: "go", PackagePath: "internal/billing", ModulePath: "example.com/app",
		Metrics: metrics.RawMetrics{DupBlocks: 3, Globals: 1}, Params: params()})
	ApplyGate(&failing, "a1b2c3d", &base, &gate.Result{
		Violations: []gate.Violation{
			{Metric: "dup_blocks", Base: 1, Head: 3, HasBase: true, Limit: "max_delta +0", Suggestion: "Extract helpers."},
			{Metric: "globals", Head: 1, HasBase: true, Limit: "max_delta +0", Suggestion: "Pass it explicitly."},
		},
		Warnings: []gate.Warning{
			{Metric: "tokens_est", Head: 24100, Limit: "max 30000", Suggestion: "at 80% of the 30000 ceiling; plan a split."},
		},
	})
	basePasses := 0.1
	warned := Build(&Input{Language: "go", PackagePath: "big", ModulePath: "example.com/app",
		Metrics: metrics.RawMetrics{TokensEst: 25000}, Params: params()})
	ApplyGate(&warned, "a1b2c3d", nil, &gate.Result{
		Passed: true,
		Warnings: []gate.Warning{
			{Metric: "tokens_est", Head: 25000, Limit: "max 30000", Suggestion: "at 83% of the 30000 ceiling."},
		},
	})
	return &Check{
		Packages: []CheckedPackage{{Report: failing, BaseAgentPasses: &basePasses}, {Report: warned}},
		Deleted:  []string{"old"},
	}
}

// passingCheck returns a check whose only package passed with no findings.
func passingCheck() *Check {
	r := Build(&Input{Language: "go", PackagePath: ".", ModulePath: "example.com/app", Params: params()})
	ApplyGate(&r, "a1b2c3d", &metrics.RawMetrics{}, &gate.Result{Passed: true})
	zero := 0.0
	return &Check{Packages: []CheckedPackage{{Report: r, BaseAgentPasses: &zero}}}
}

func TestApplyGate(t *testing.T) {
	t.Parallel()

	c := fixedCheck()
	r := c.Packages[0].Report
	if r.Passed == nil || *r.Passed || r.Baseline == nil || r.Baseline.Ref != "a1b2c3d" || r.Baseline.Metrics.DupBlocks != 1 {
		t.Fatalf("report = passed %v baseline %+v, want failed against a1b2c3d", r.Passed, r.Baseline)
	}
	if len(r.Violations) != 2 || r.Violations[0].Base == nil || *r.Violations[0].Base != 1 {
		t.Errorf("violations = %+v, want dup_blocks with base 1 first of two", r.Violations)
	}
	w := c.Packages[1].Report
	if w.Passed == nil || !*w.Passed || w.Baseline != nil || w.Violations == nil || len(w.Violations) != 0 || len(w.Warnings) != 1 {
		t.Errorf("new package = passed %v baseline %+v violations %v warnings %v, want passed, no baseline, "+
			"an empty non-nil violations list, one warning",
			w.Passed, w.Baseline, w.Violations, w.Warnings)
	}
	if !c.Failed() || passingCheck().Failed() {
		t.Error("Failed does not reflect violations")
	}
}

func TestWriteCheckText(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WriteCheckText(&buf, fixedCheck()); err != nil {
		t.Fatal(err)
	}
	want := "violations:\n" +
		"  internal/billing\n" +
		"    dup_blocks: 1 -> 3, max_delta +0. Extract helpers.\n" +
		"    globals: 0 -> 1, max_delta +0. Pass it explicitly.\n" +
		"warnings:\n" +
		"  internal/billing\n" +
		"    tokens_est: 24100 (no baseline), max 30000. at 80% of the 30000 ceiling; plan a split.\n" +
		"  big\n" +
		"    tokens_est: 25000 (no baseline), max 30000. at 83% of the 30000 ceiling.\n" +
		"internal/billing: 0.0 passes (ONE_PASS), 2 violations, 1 warning, -0.1 passes from baseline\n" +
		"big: 1.0 passes (ONE_PASS), 0 violations, 1 warning, new since baseline\n" +
		"deleted since baseline: old\n"
	if buf.String() != want {
		t.Errorf("text =\n%s\nwant\n%s", buf.String(), want)
	}
}

func TestFindingTextChangedFunction(t *testing.T) {
	t.Parallel()

	f := Finding{Metric: "changed_func_cognitive_max", Head: 40, Limit: "max 30", Suggestion: "Split grade."}
	want := "changed_func_cognitive_max: 40 (changed since baseline), max 30. Split grade."
	if got := findingText(&f); got != want {
		t.Errorf("findingText = %q, want %q", got, want)
	}
}

func TestWriteCheckTextSummaryDelta(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		base float64
		want string
	}{
		{"no change", 0, ", +0.0 passes from baseline\n"},
		{"improved", 0.4, ", -0.4 passes from baseline\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := passingCheck()
			c.Packages[0].BaseAgentPasses = &tt.base
			var buf bytes.Buffer
			if err := WriteCheckText(&buf, c); err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(buf.String(), tt.want) || !strings.HasPrefix(buf.String(), ".: 0.0 passes (ONE_PASS), 0 violations, 0 warnings") {
				t.Errorf("text = %q, want the summary to end with %q", buf.String(), tt.want)
			}
		})
	}
}

func TestWriteCheckJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WriteCheckJSON(&buf, fixedCheck()); err != nil {
		t.Fatal(err)
	}
	var got []map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decoding %s: %v", buf.String(), err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d reports, want 2", len(got))
	}
	for _, key := range []string{"baseline", "violations", "warnings", "passed"} {
		if _, ok := got[0][key]; !ok {
			t.Errorf("failing report has no %q key", key)
		}
	}
	if string(got[1]["violations"]) != "[]" {
		t.Errorf("passing report violations = %s, want []", got[1]["violations"])
	}
	if string(got[0]["passed"]) != "false" || string(got[1]["passed"]) != "true" {
		t.Errorf("passed = %s, %s, want false, true", got[0]["passed"], got[1]["passed"])
	}

	buf.Reset()
	if err := WriteCheckJSON(&buf, &Check{}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "[]\n" {
		t.Errorf("empty check = %q, want %q", buf.String(), "[]\n")
	}
}

func TestWriteHook(t *testing.T) {
	t.Parallel()

	t.Run("failure blocks with every finding", func(t *testing.T) {
		t.Parallel()

		var out, warn bytes.Buffer
		if err := WriteHook(&out, &warn, fixedCheck()); err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(&out)
		dec.DisallowUnknownFields()
		var got hookBlock
		if err := dec.Decode(&got); err != nil {
			t.Fatalf("decoding %q: %v", out.String(), err)
		}
		if _, err := dec.Token(); err != io.EOF {
			t.Errorf("hook output has more than one JSON value: %q", out.String())
		}
		if got.Decision != "block" {
			t.Errorf("decision = %q, want block", got.Decision)
		}
		for _, s := range []string{"dup_blocks: 1 -> 3", "globals: 0 -> 1", "warnings:", "tokens_est: 25000"} {
			if !strings.Contains(got.Reason, s) {
				t.Errorf("reason %q does not contain %q", got.Reason, s)
			}
		}
		if warn.Len() != 0 {
			t.Errorf("warnings writer = %q, want empty on failure", warn.String())
		}
	})
	t.Run("success prints an empty object and warnings aside", func(t *testing.T) {
		t.Parallel()

		c := fixedCheck()
		c.Packages = c.Packages[1:]
		var out, warn bytes.Buffer
		if err := WriteHook(&out, &warn, c); err != nil {
			t.Fatal(err)
		}
		if out.String() != "{}\n" {
			t.Errorf("stdout = %q, want %q", out.String(), "{}\n")
		}
		if !strings.Contains(warn.String(), "tokens_est: 25000") {
			t.Errorf("warnings = %q, want the tokens_est warning", warn.String())
		}
	})
	t.Run("clean success writes no warnings", func(t *testing.T) {
		t.Parallel()

		var out, warn bytes.Buffer
		if err := WriteHook(&out, &warn, passingCheck()); err != nil {
			t.Fatal(err)
		}
		if out.String() != "{}\n" || warn.Len() != 0 {
			t.Errorf("stdout = %q, warnings = %q, want {} and nothing", out.String(), warn.String())
		}
	})
}

func TestWriteGitHub(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WriteGitHub(&buf, fixedCheck()); err != nil {
		t.Fatal(err)
	}
	want := "::error file=internal/billing::dup_blocks: 1 -> 3, max_delta +0. Extract helpers.\n" +
		"::error file=internal/billing::globals: 0 -> 1, max_delta +0. Pass it explicitly.\n" +
		"::warning file=internal/billing::tokens_est: 24100 (no baseline), max 30000. at 80%25 of the 30000 ceiling; plan a split.\n" +
		"::warning file=big::tokens_est: 25000 (no baseline), max 30000. at 83%25 of the 30000 ceiling.\n"
	if buf.String() != want {
		t.Errorf("github =\n%s\nwant\n%s", buf.String(), want)
	}
}

// TestWriteGitHubLocations annotates a fixed check of a module below the
// repository root: located findings on their file and line, an unlocated
// one on its package directory and a module-row finding on its block, all
// prefixed with the module's directory.
func TestWriteGitHubLocations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		moduleDir string
		want      string
	}{
		{name: "module below the repository root", moduleDir: "services/api", want: "" +
			"::error file=services/api/internal/billing/dupes.go,line=40::module: dup_blocks_cross_pkg: 0 -> 1, max_delta +0.\n" +
			"::error file=services/api/internal/billing/dupes.go,line=12::dup_blocks: 1 -> 3, max_delta +0. Extract helpers.\n" +
			"::error file=services/api/internal/billing::globals: 0 -> 1, max_delta +0. Pass it explicitly.\n" +
			"::warning file=services/api/internal/billing/billing.go::tokens_est: 24100 (no baseline), max 30000. at 80%25 of the 30000 ceiling; plan a split.\n" +
			"::warning file=services/api/main.go,line=1::tokens_est: 25000 (no baseline), max 30000. at 83%25 of the 30000 ceiling.\n"},
		{name: "module at the repository root", want: "" +
			"::error file=internal/billing/dupes.go,line=40::module: dup_blocks_cross_pkg: 0 -> 1, max_delta +0.\n" +
			"::error file=internal/billing/dupes.go,line=12::dup_blocks: 1 -> 3, max_delta +0. Extract helpers.\n" +
			"::error file=internal/billing::globals: 0 -> 1, max_delta +0. Pass it explicitly.\n" +
			"::warning file=internal/billing/billing.go::tokens_est: 24100 (no baseline), max 30000. at 80%25 of the 30000 ceiling; plan a split.\n" +
			"::warning file=main.go,line=1::tokens_est: 25000 (no baseline), max 30000. at 83%25 of the 30000 ceiling.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := fixedCheck()
			c.ModuleDir = tt.moduleDir
			billing := &c.Packages[0].Report
			billing.Violations[0].Location = &Location{File: "internal/billing/dupes.go", Line: 12}
			billing.Warnings[0].Location = &Location{File: "internal/billing/billing.go"} // a file without a line
			root := &c.Packages[1].Report
			root.PackagePath = "."
			root.Warnings[0].Location = &Location{File: "main.go", Line: 1}
			zero, one := 0, 1
			mod := Build(&Input{Language: "go", PackagePath: metrics.ModuleRowID, ModulePath: "example.com/app",
				Metrics: metrics.RawMetrics{DupBlocksCrossPkg: &one}, Params: params()})
			ApplyGate(&mod, "a1b2c3d", &metrics.RawMetrics{DupBlocksCrossPkg: &zero}, &gate.Result{
				Violations: []gate.Violation{{Metric: "dup_blocks_cross_pkg", Head: 1, HasBase: true, Limit: "max_delta +0"}},
			})
			mod.Violations[0].Location = &Location{File: "internal/billing/dupes.go", Line: 40}
			c.Module = &CheckedPackage{Report: mod}

			var buf bytes.Buffer
			if err := WriteGitHub(&buf, c); err != nil {
				t.Fatal(err)
			}
			if buf.String() != tt.want {
				t.Errorf("github =\n%s\nwant\n%s", buf.String(), tt.want)
			}
		})
	}
}

// tokenizerCheck returns fixedCheck with each baseline block marked as
// counted with baseTokenizer by a check counting with est.
func tokenizerCheck(baseTokenizer string) *Check {
	c := fixedCheck()
	c.Tokenizer = "est"
	for i := range c.Packages {
		MarkTokenizer(&c.Packages[i].Report, baseTokenizer, c.Tokenizer)
	}
	return c
}

func TestMarkTokenizer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		base           string
		wantComparable string
	}{
		{name: "same tokenizer", base: "est", wantComparable: "true"},
		{name: "other tokenizer", base: "o200k", wantComparable: "false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := tokenizerCheck(tt.base)
			if c.Packages[1].Report.Baseline != nil {
				t.Error("MarkTokenizer added a baseline block to a package without one")
			}
			var buf bytes.Buffer
			if err := WriteCheckJSON(&buf, c); err != nil {
				t.Fatal(err)
			}
			var got []struct {
				Baseline map[string]json.RawMessage `json:"baseline"`
			}
			if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
				t.Fatalf("decoding %s: %v", buf.String(), err)
			}
			b := got[0].Baseline
			if string(b["tokenizer"]) != `"`+tt.base+`"` || string(b["tokens_comparable"]) != tt.wantComparable {
				t.Errorf("baseline tokenizer %s, tokens_comparable %s; want %q, %s",
					b["tokenizer"], b["tokens_comparable"], tt.base, tt.wantComparable)
			}
		})
	}
}

// TestTokenizerNote checks that the hook and GitHub formats say when the
// baseline's token counts are not comparable, and stay silent otherwise.
func TestTokenizerNote(t *testing.T) {
	t.Parallel()

	const note = "baseline tokenizer o200k differs from check tokenizer est; token counts are not comparable"
	for _, base := range []string{"est", "o200k"} {
		t.Run(base, func(t *testing.T) {
			t.Parallel()

			want := base != "est"
			var gh bytes.Buffer
			if err := WriteGitHub(&gh, tokenizerCheck(base)); err != nil {
				t.Fatal(err)
			}
			if got := strings.HasPrefix(gh.String(), "::warning title=astimate::"+note+"\n"); got != want {
				t.Errorf("github leads with the tokenizer warning = %v, want %v:\n%s", got, want, gh.String())
			}
			var out, warn bytes.Buffer
			if err := WriteHook(&out, &warn, tokenizerCheck(base)); err != nil {
				t.Fatal(err)
			}
			var block hookBlock
			if err := json.Unmarshal(out.Bytes(), &block); err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(block.Reason, "warning: "+note+"\n"); got != want {
				t.Errorf("hook reason has the tokenizer warning = %v, want %v:\n%s", got, want, block.Reason)
			}
		})
	}
	t.Run("hook success", func(t *testing.T) {
		t.Parallel()

		c := passingCheck()
		c.Tokenizer = "est"
		MarkTokenizer(&c.Packages[0].Report, "o200k", "est")
		var out, warn bytes.Buffer
		if err := WriteHook(&out, &warn, c); err != nil {
			t.Fatal(err)
		}
		if out.String() != "{}\n" || warn.String() != "warning: "+note+"\n" {
			t.Errorf("stdout = %q, warnings = %q, want {} and the tokenizer warning", out.String(), warn.String())
		}
	})
}

func TestWorkflowEscapes(t *testing.T) {
	t.Parallel()

	if got, want := escapeData("50%\r\nnext: a,b"), "50%25%0D%0Anext: a,b"; got != want {
		t.Errorf("escapeData = %q, want %q", got, want)
	}
	if got, want := escapeProperty("a:b,c%\n"), "a%3Ab%2Cc%25%0A"; got != want {
		t.Errorf("escapeProperty = %q, want %q", got, want)
	}
}

func TestFindingTextBooleans(t *testing.T) {
	t.Parallel()

	base := 1.0
	f := Finding{Metric: "has_tests", Base: &base, Head: 0, Limit: "require true"}
	if got, want := findingText(&f), "has_tests: true -> false, require true."; got != want {
		t.Errorf("findingText = %q, want %q", got, want)
	}
}
