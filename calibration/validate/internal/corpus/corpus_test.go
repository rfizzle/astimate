package corpus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/replay/labels"
	"github.com/rfizzle/astimate/internal/metrics"
)

// writeReplay writes a replay directory and a labels file under t's temp
// directory and returns their paths.
func writeReplay(t *testing.T, commits, packages, labelsYAML string) Source {
	t.Helper()
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	if err := os.Mkdir(data, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(data, "commits.jsonl"):  commits,
		filepath.Join(data, "packages.jsonl"): packages,
		filepath.Join(dir, "labels.yaml"):     labelsYAML,
	}
	for p, s := range files {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Source{Name: "fixture", Data: data, Labels: filepath.Join(dir, "labels.yaml")}
}

const fixtureCommits = `{"commit":"c1","subject":"root","config_version":"v1","baseline":"empty","loaded":true,"passed":false}
{"commit":"c2","subject":"broken","config_version":"v1","baseline":"parent","loaded":false,"passed":null}
{"commit":"c3","subject":"human","config_version":"v1","baseline":"parent","loaded":true,"passed":true}

{"commit":"c4","subject":"grow","config_version":"v1","baseline":"parent","loaded":true,"passed":true}
`

const fixturePackages = `{"commit":"c1","package":"<module>","language":"go","metrics":{"dup_blocks_cross_pkg":2},"base":null,"sloc_delta":0,"violations":[{"metric":"dup_blocks_cross_pkg"}]}
{"commit":"c1","package":"a","language":"go","metrics":{"sloc":40},"base":null,"sloc_delta":40,"violations":[]}
{"commit":"c4","package":"a","language":"go","metrics":{"sloc":60},"base":{"sloc":40},"sloc_delta":20,"violations":[]}
`

const fixtureLabels = `source: {repository: r, range: x, data: d}
commits:
  - {hash: c1, verdict: block, reason: fixed up by c9, provenance: rule, agent: true, fired: [{part: fixup, by: c9}]}
  - {hash: c2, verdict: allow, reason: r, provenance: rule, agent: true}
  - {hash: c3, verdict: allow, reason: r, provenance: rule, agent: false}
  - {hash: c4, verdict: allow, reason: r, provenance: rule}
`

func TestLoad(t *testing.T) {
	src := writeReplay(t, fixtureCommits, fixturePackages, fixtureLabels)
	for _, agentOnly := range []bool{true, false} {
		c, err := Load(src, agentOnly)
		if err != nil {
			t.Fatal(err)
		}
		want := 2
		if !agentOnly {
			want = 3
		}
		if len(c.Commits) != want || c.NotLoaded != 1 || c.Labeled != 4 || c.ConfigVersion != "v1" || !c.RuleLabels {
			t.Fatalf("agentOnly %v: %+v", agentOnly, c)
		}
		if agentOnly && c.NotAgent != 1 {
			t.Errorf("not agent %d", c.NotAgent)
		}
		root := c.Commits[0]
		if root.Hash != "c1" || !root.Block() || !root.Failed() || root.Passed || len(root.Rows) != 2 {
			t.Fatalf("root %+v", root)
		}
		mod := root.Rows[0]
		if !mod.Module() || mod.Base == nil || *mod.Base.DupBlocksCrossPkg != 0 || mod.Violated[0] != "dup_blocks_cross_pkg" {
			t.Errorf("empty-baseline module row %+v", mod)
		}
		if root.Rows[1].Base != nil || root.Rows[1].Module() {
			t.Error("a new package row got a base")
		}
		grow := c.Commits[len(c.Commits)-1]
		if grow.Rows[0].Base.SLOC != 40 || grow.Rows[0].SLOCDelta != 20 || grow.Block() {
			t.Errorf("grow %+v", grow.Rows[0])
		}
		if b, a := c.Counts(); b != 1 || a != want-1 {
			t.Errorf("counts %d %d", b, a)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name, commits, packages, labels, want string
	}{
		{"verdict disagrees", strings.Replace(fixtureCommits, `"passed":false`, `"passed":true`, 1), fixturePackages, fixtureLabels, "passed is true"},
		{"mixed configs", strings.Replace(fixtureCommits, `"subject":"grow","config_version":"v1"`, `"subject":"grow","config_version":"v2"`, 1), fixturePackages, fixtureLabels, "gated under v2"},
		{"label missing", fixtureCommits, fixturePackages, strings.Replace(fixtureLabels, "  - {hash: c4, verdict: allow, reason: r, provenance: rule}\n", "", 1), "c4 has no label"},
		{"bad row", fixtureCommits, fixturePackages + "{not json\n", fixtureLabels, "packages.jsonl line 4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeReplay(t, tc.commits, tc.packages, tc.labels), true)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
	if _, err := Load(Source{Data: t.TempDir(), Labels: filepath.Join(t.TempDir(), "none.yaml")}, true); err == nil {
		t.Error("missing labels loaded")
	}
}

func TestParseSource(t *testing.T) {
	cases := []struct {
		in   string
		want Source
		ok   bool
	}{
		{"a=data/x:labels/a.yaml", Source{Name: "a", Data: "data/x", Labels: "labels/a.yaml"}, true},
		{"a=C:/x:l.yaml", Source{Name: "a", Data: "C:/x", Labels: "l.yaml"}, true},
		{"data:labels", Source{}, false},
		{"=d:l", Source{}, false},
		{"a=d:", Source{}, false},
		{"a=:l", Source{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseSource(tc.in)
			if (err == nil) != tc.ok || got != tc.want {
				t.Errorf("got %+v, %v", got, err)
			}
		})
	}
}

func TestCapacityOnly(t *testing.T) {
	capacity := func(m string) bool { return m == "sloc" || m == "tokens_est" }
	cases := []struct {
		name  string
		label labels.Label
		want  bool
	}{
		{"capacity", labels.Label{Verdict: labels.Block, Metrics: []string{"sloc", "tokens_est"}, Reason: "adds 200 SLOC"}, true},
		{"fixed up", labels.Label{Verdict: labels.Block, Metrics: []string{"sloc"}, Reason: "fixed up by abc"}, false},
		{"reverted", labels.Label{Verdict: labels.Block, Metrics: []string{"sloc"}, Reason: "reverted by abc"}, false},
		{"rule evidence", labels.Label{Verdict: labels.Block, Metrics: []string{"sloc"}, Reason: "x", Fired: []labels.Evidence{{Part: labels.PartFixup, By: "b"}}}, false},
		{"mixed", labels.Label{Verdict: labels.Block, Metrics: []string{"sloc", "dup_blocks"}, Reason: "x"}, false},
		{"no rules", labels.Label{Verdict: labels.Block, Reason: "x"}, false},
		{"allow", labels.Label{Verdict: labels.Allow, Metrics: []string{"sloc"}, Reason: "x"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Commit{Label: tc.label}
			if got := c.CapacityOnly(capacity); got != tc.want {
				t.Errorf("got %v", got)
			}
		})
	}
}

func TestWithoutAndPool(t *testing.T) {
	a := &Corpus{Name: "a", Labeled: 3, NotLoaded: 1, RuleLabels: true, ConfigVersion: "v", Commits: []Commit{
		{Hash: "1", Label: labels.Label{Verdict: labels.Block}},
		{Hash: "2", Label: labels.Label{Verdict: labels.Allow}, Rows: []Row{{Package: metrics.ModuleRowID, Violated: []string{"x"}}}},
	}}
	b := &Corpus{Name: "b", Labeled: 1, NotAgent: 2, Commits: []Commit{{Hash: "3", Label: labels.Label{Verdict: labels.Block}}}}
	w := a.Without("a'", func(c *Commit) bool { return c.Hash == "1" })
	if w.Name != "a'" || len(w.Commits) != 1 || len(a.Commits) != 2 || !w.Commits[0].Failed() {
		t.Errorf("without %+v", w)
	}
	p := Pool("p", a, b)
	if len(p.Commits) != 3 || p.Labeled != 4 || p.NotLoaded != 1 || p.NotAgent != 2 || p.RuleLabels || p.ConfigVersion != "v" {
		t.Errorf("pool %+v", p)
	}
	if !Pool("q", a).RuleLabels {
		t.Error("a pool of rule-labeled corpora is not rule-labeled")
	}
}
