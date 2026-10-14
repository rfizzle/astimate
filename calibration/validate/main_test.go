package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/validate/internal/measure"
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
// and a rule-labeled corpus, both replayed under the default, under dir.
func corpora(t *testing.T, dir string) (hand, rule string) {
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

	hand = fixture(t, dir, "hand",
		c("h1", false)+c("h2", true)+c("h3", false),
		`{"commit":"h1","package":"a","language":"go","metrics":{"sloc":1500,"has_tests":true},"base":null,"sloc_delta":1500,"violations":[{"metric":"sloc"}]}
{"commit":"h3","package":"a","language":"go","metrics":{"sloc":100,"dup_blocks":2,"duplication_pct":12,"has_tests":true},"base":{"sloc":90,"dup_blocks":1,"duplication_pct":2,"has_tests":true},"sloc_delta":10,"violations":[{"metric":"dup_blocks","also":["duplication_pct"]}]}
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
	hand, rule := corpora(t, t.TempDir())
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
	hand, _ := corpora(t, t.TempDir())
	other := filepath.Join(t.TempDir(), "astimate.yaml")
	def, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	cfg := strings.Replace(string(config.Default()), "config_version: "+def.Version, "config_version: other-1", 1)
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

// TestReevaluated matches rows replayed under another committed
// configuration to the rules the configuration validated with runs at
// other limits, and refuses one that measures differently or has other
// rules. A replay at another min_tokens_cross is accepted only from rows
// that record the added cross-package blocks, with the cross rule
// re-evaluated.
func TestReevaluated(t *testing.T) {
	data := string(config.Default())
	def, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// edit returns the default with old replaced by repl, failing when
	// the default lacks old.
	edit := func(old, repl string) string {
		t.Helper()
		if !strings.Contains(data, old) {
			t.Fatalf("the embedded default has no %q", old)
		}
		return strings.Replace(data, old, repl, 1)
	}
	version := "config_version: " + def.Version + "\n"
	replays := map[string]string{
		"same-1":      edit(version, "config_version: same-1\n"),
		"warned-1":    edit("  - metric: init_funcs\n    kind: density\n", "  - metric: init_funcs\n    kind: density\n    severity: warn\n"),
		"limits-1":    edit("    max: 16000\n", "    max: 17000\n"),
		"tokens-1":    edit("  min_tokens: 60\n", "  min_tokens: 70\n"),
		"floor-1":     edit("  min_tokens_cross: 120\n", "  min_tokens_cross: 90\n"),
		"reordered-1": edit("  - metric: init_funcs\n", "  - metric: globals\n"),
	}
	for name, text := range replays {
		text = strings.Replace(text, version, "config_version: "+name+"\n", 1)
		if err := os.WriteFile(filepath.Join(dir, "astimate-"+name+".yaml"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rules := measure.NewRules(def)
	tokens, cross := -1, -1
	for i, r := range rules.List {
		switch {
		case r.Language == "" && r.Threshold.Metric == "tokens_est":
			tokens = i
		case r.Threshold.Metric == "dup_blocks_cross_pkg":
			cross = i
		}
	}
	tests := []struct {
		name    string
		version string
		// records says the rows record their added cross-package blocks.
		records bool
		want    []int
		err     string
	}{
		{"same version", def.Version, false, nil, ""},
		{"another version, the same rules", "same-1", false, nil, ""},
		{"a rule that differs only in severity", "warned-1", false, nil, ""},
		{"a rule at another limit", "limits-1", false, []int{tokens}, ""},
		{"other duplication settings", "tokens-1", true, nil, "duplication settings"},
		{"another cross floor, rows with added blocks", "floor-1", true, []int{cross}, ""},
		{"another cross floor, rows without them", "floor-1", false, nil, "record no added cross-package blocks"},
		{"a rule of another kind", "reordered-1", false, nil, "where"},
		{"no committed configuration", "thresholds-1999-01-01", false, nil, "cannot be read"},
	}
	code, err := config.Parse([]byte(edit("  - metric: dup_blocks\n", "  - metric: dup_blocks_code\n")))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("a rule on another metric in a replayed rule's place", func(t *testing.T) {
		codeRules := measure.NewRules(code)
		got, err := reevaluated(code, codeRules, "same-1", dir, false)
		if err != nil || len(got) != 1 {
			t.Fatalf("reevaluated = %v, %v, want one rule", got, err)
		}
		if r := codeRules.List[got[0]]; r.Threshold.Metric != "dup_blocks_code" || r.Replaced != "dup_blocks" || !r.Reevaluated {
			t.Errorf("rule %+v, want dup_blocks_code replacing dup_blocks", r)
		}
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := reevaluated(def, rules, tt.version, dir, tt.records)
			switch {
			case tt.err != "" && (err == nil || !strings.Contains(err.Error(), tt.err)):
				t.Errorf("err = %v, want %q", err, tt.err)
			case tt.err == "" && err != nil:
				t.Errorf("err = %v", err)
			case !reflect.DeepEqual(got, tt.want):
				t.Errorf("reevaluated(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

// TestRunCodeBlocks validates a configuration that gates dup_blocks_code
// on rows a dup_blocks rule gated, each row storing both counts: the
// commit that added only a self-overlap block passes, the code paste
// still fails, and the Method names the rule and the metric it replaced.
func TestRunCodeBlocks(t *testing.T) {
	t.Chdir(t.TempDir())
	def := string(config.Default())
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join("calibration", "thresholds"), 0o755); err != nil {
		t.Fatal(err)
	}
	replayed := strings.Replace(def, "config_version: "+cfg.Version+"\n", "config_version: blocks-1\n", 1)
	code := strings.Replace(def, "  - metric: dup_blocks\n", "  - metric: dup_blocks_code\n", 1)
	if replayed == def || code == def {
		t.Fatal("the embedded default has no config_version or dup_blocks rule to edit")
	}
	if err := os.WriteFile(filepath.Join("calibration", "thresholds", "astimate-blocks-1.yaml"), []byte(replayed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("code.yaml", []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	c := func(hash string) string {
		return `{"commit":"` + hash + `","subject":"subject ` + hash + `","config_version":"blocks-1","baseline":"parent","loaded":true,"passed":false}` + "\n"
	}
	row := func(hash, base, head string) string {
		return `{"commit":"` + hash + `","package":"a","language":"go","metrics":{"sloc":200,"has_tests":true,` + head +
			`},"base":{"sloc":190,"has_tests":true,` + base + `},"sloc_delta":10,"violations":[{"metric":"dup_blocks"}]}` + "\n"
	}
	kinds := fixture(t, ".", "kinds", c("k1")+c("k2"),
		row("k1", `"dup_blocks":1,"dup_blocks_code":1`, `"dup_blocks":2,"dup_blocks_code":1`)+
			row("k2", `"dup_blocks":1,"dup_blocks_code":0`, `"dup_blocks":2,"dup_blocks_code":1`),
		`source: {repository: r, range: x, data: d}
commits:
  - {hash: k1, verdict: allow, reason: a run of calls matching itself a line down, provenance: confirmed, agent: true}
  - {hash: k2, verdict: block, reason: a pasted function, provenance: confirmed, agent: true}
`)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--corpus", kinds, "--config", "code.yaml", "--out", "report.md", "--date", "2026-10-14"}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v; stderr %s", err, stderr.String())
	}
	data, err := os.ReadFile("report.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"| kinds | 1 of 1 (100.0%) | met | 0 of 1 (0.0%) | met |",
		"`dup_blocks_code` (on `dup_blocks` in the replay)",
		"at other limits or on another metric.",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("report lacks %q", want)
		}
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
