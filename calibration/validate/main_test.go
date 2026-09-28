package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
)

// fixture writes a replay directory and labels file named name under dir
// and returns the --corpus value for them.
func fixture(t *testing.T, dir, name, commits, packages, labelsYAML string) string {
	t.Helper()
	data := filepath.Join(dir, name)
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	for p, s := range map[string]string{"commits.jsonl": commits, "packages.jsonl": packages} {
		if err := os.WriteFile(filepath.Join(data, p), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lf := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(lf, []byte(labelsYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return name + "=" + data + ":" + lf
}

// corpora writes a hand-labeled corpus with one capacity-only block label
// and a rule-labeled corpus, both replayed under the default.
func corpora(t *testing.T) (hand, rule string) {
	t.Helper()
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	c := func(hash string, passed bool) string {
		p := "true"
		if !passed {
			p = "false"
		}
		return `{"commit":"` + hash + `","subject":"subject ` + hash + `","config_version":"` + cfg.Version + `","baseline":"parent","loaded":true,"passed":` + p + "}\n"
	}
	dir := t.TempDir()
	hand = fixture(t, dir, "hand",
		c("h1", false)+c("h2", true)+c("h3", false),
		`{"commit":"h1","package":"a","language":"go","metrics":{"sloc":1500,"has_tests":true},"base":null,"sloc_delta":1500,"violations":[{"metric":"sloc"}]}
{"commit":"h3","package":"a","language":"go","metrics":{"sloc":100,"dup_blocks":2,"has_tests":true},"base":{"sloc":90,"dup_blocks":1,"has_tests":true},"sloc_delta":10,"violations":[{"metric":"dup_blocks"}]}
`,
		`source: {repository: r, range: x, data: d}
commits:
  - {hash: h1, verdict: block, reason: lands a package over the ceiling, metrics: [sloc], provenance: proposed, agent: true}
  - {hash: h2, verdict: block, reason: fixed up by h9, provenance: proposed, agent: true}
  - {hash: h3, verdict: allow, reason: fine, provenance: proposed, agent: true}
`)
	rule = fixture(t, dir, "rule",
		c("r1", false)+c("r2", true),
		`{"commit":"r1","package":"b","language":"go","metrics":{"sloc":50,"dup_blocks":1,"has_tests":true},"base":{"sloc":40,"has_tests":true},"sloc_delta":10,"violations":[{"metric":"dup_blocks"}]}
`,
		`source: {repository: r, range: x, data: d}
commits:
  - {hash: r1, verdict: block, reason: fixed later, provenance: rule, agent: true, fired: [{part: fixup, by: r9}]}
  - {hash: r2, verdict: allow, reason: no fix, provenance: rule, agent: true}
`)
	return hand, rule
}

func TestRun(t *testing.T) {
	hand, rule := corpora(t)
	out := filepath.Join(t.TempDir(), "report.md")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--corpus", hand, "--corpus", rule, "--out", out, "--date", "2026-09-28"}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v; stderr %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "all corpora pooled, capacity-only set aside recall 50.0%, false failures 50.0%") {
		t.Errorf("stdout %q", stdout.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	report := string(data)
	for _, want := range []string{
		"| hand | 1 of 2 (50.0%) | not met | 1 of 1 (100.0%) | not met |",
		"| hand, capacity-only set aside | 0 of 1 (0.0%) | not met |",
		"| all corpora pooled | 2 of 3 (66.7%) |",
		"| hand | hand | 3 | 0 | 0 | 2 | 1 | 1 |",
		"`hand` has 1 capacity-only `block` labels",
		"| `h2` | subject h2 | fixed up by h9 |",
		"### rule\n\nNone.",
		"0 of ",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}

func TestRunErrors(t *testing.T) {
	hand, _ := corpora(t)
	other := filepath.Join(t.TempDir(), "astimate.yaml")
	cfg := strings.Replace(string(config.Default()), "config_version: thresholds-", "config_version: other-", 1)
	if err := os.WriteFile(other, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		args  []string
		usage bool
		want  string
	}{
		{"no corpus", nil, true, "at least one --corpus"},
		{"bad corpus", []string{"--corpus", "nope"}, true, "not name="},
		{"bad date", []string{"--corpus", hand, "--date", "yesterday"}, true, "--date"},
		{"extra argument", []string{"--corpus", hand, "extra"}, true, `unexpected argument "extra"`},
		{"other config", []string{"--corpus", hand, "--config", other, "--out", filepath.Join(t.TempDir(), "r.md")}, false, "replayed under"},
		{"unwritable", []string{"--corpus", hand, "--out", filepath.Join(t.TempDir(), "no", "r.md")}, false, "writing the report"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tc.args, &stdout, &stderr)
			var u usageError
			if err == nil || errors.As(err, &u) != tc.usage || !strings.Contains(err.Error()+stderr.String(), tc.want) {
				t.Errorf("err = %v, stderr %q", err, stderr.String())
			}
		})
	}
}

func TestDefaultOut(t *testing.T) {
	o := newOptions()
	if err := o.parse([]string{"--corpus", "a=b:c", "--date", "2026-01-02"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if o.out != "calibration/reports/gate-validation-2026-01-02.md" || !o.agentOnly || o.criteria.Budget != 0.1 {
		t.Errorf("options %+v", o)
	}
}
