package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
)

// TestRunWarnRule validates the fixture corpora under the default with the
// dup_blocks rule marked severity: warn, beside a corpus replayed under
// that configuration, which recorded the rule's breach as a warning with
// severity warn. The gate's verdict ignores the warn rule: the commits
// only dup_blocks failed as replayed now pass, and the one it warned on
// stays passed. The rule keeps its own row with its precision and recall,
// counted from the breaches recorded under either severity.
func TestRunWarnRule(t *testing.T) {
	dir := t.TempDir()
	hand, rule := corpora(t, dir)
	def, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	warned := fixture(t, dir, "warned",
		`{"commit":"w1","subject":"subject w1","config_version":"`+def.Version+`","baseline":"parent","loaded":true,"passed":true}
{"commit":"w2","subject":"subject w2","config_version":"`+def.Version+`","baseline":"parent","loaded":true,"passed":true}
`,
		`{"commit":"w1","package":"c","language":"go","metrics":{"sloc":50,"dup_blocks":1,"has_tests":true},"base":{"sloc":40,"has_tests":true},"sloc_delta":10,"violations":[],"warnings":[{"metric":"dup_blocks","severity":"warn"}]}
{"commit":"w2","package":"c","language":"go","metrics":{"sloc":60,"dup_blocks":1,"has_tests":true},"base":{"sloc":50,"dup_blocks":1,"has_tests":true},"sloc_delta":10,"violations":[],"warnings":[]}
`,
		`source: {repository: r, range: x, data: d}
commits:
  - {hash: w1, verdict: block, reason: copied a helper, provenance: proposed, agent: true}
  - {hash: w2, verdict: allow, reason: fine, provenance: proposed, agent: true}
`)
	const dupRule = "  - metric: dup_blocks\n    kind: density\n"
	data := string(config.Default())
	if !strings.Contains(data, dupRule) {
		t.Fatalf("the embedded default has no %q", dupRule)
	}
	cfg := filepath.Join(dir, "warn.yaml")
	if err := os.WriteFile(cfg, []byte(strings.Replace(data, dupRule, dupRule+"    severity: warn\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "report.md")
	var stdout, stderr bytes.Buffer
	args := []string{"--corpus", hand, "--corpus", rule, "--corpus", warned, "--config", cfg, "--out", out, "--date", "2026-09-29"}
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v; stderr %s", err, stderr.String())
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	report := string(got)
	for _, want := range []string{
		// The gate's verdict: h3 (allow) and r1 (block), which only
		// dup_blocks failed as replayed, pass; h1 still fails on sloc.
		"| hand | 1 of 2 (50.0%) | not met | 0 of 1 (0.0%) | met |",
		"| rule | 0 of 1 (0.0%) | not met | 0 of 1 (0.0%) | met |",
		"| warned | 0 of 1 (0.0%) | not met | 0 of 1 (0.0%) | met |",
		// Pooled, the warn rule's own rates: it fired on h3 (allow), r1
		// and w1 (block) of 4 block and 3 allow commits; the gate's row
		// counts h1 alone.
		"| `dup_blocks (warn)` | 2 | 1 | 66.7% | 50.0% | 33.3% |",
		"| gate (any `fail` rule) | 1 | 0 | 100.0% | 25.0% | 0.0% |",
		"| all corpora pooled | 1 of 4 (25.0%) | not met | 0 of 3 (0.0%) | met |",
		"A rule marked `(warn)` has `severity: warn`",
		"- A rule with `severity: warn`, marked `(warn)`, reports its breaches as warnings",
		// The commits the gate now misses include the warn rule's.
		"| `r1` | subject r1 |",
		"| `w1` | subject w1 |",
		// Every recorded breach, the warning included, re-evaluates.
		"reproduces the recorded violations: 0 of ",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q", want)
		}
	}
	if strings.Contains(report, "gate (any rule)") {
		t.Error("report labels the gate row as any rule while a warn rule is configured")
	}
	if t.Failed() {
		t.Logf("report:\n%s", report)
	}
}
