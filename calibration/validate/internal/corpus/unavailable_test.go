package corpus

import "testing"

// TestLoadSetsAsideUnavailableAndReadsCross loads a commit failed only on a
// package row whose package did not load at the parent, which is set aside
// with its violation so the commit passes, and a commit whose module row
// records the cross-package blocks it may have added.
func TestLoadSetsAsideUnavailableAndReadsCross(t *testing.T) {
	const commits = `{"commit":"u1","subject":"broken parent","config_version":"v1","baseline":"parent","loaded":true,"passed":false}
{"commit":"x1","subject":"copy","config_version":"v1","baseline":"parent","loaded":true,"passed":true}
`
	const packages = `{"commit":"u1","package":"a","language":"go","new":true,"baseline":"unavailable","metrics":{"sloc":1200},"base":null,"sloc_delta":1200,"violations":[{"metric":"sloc"}]}
{"commit":"u1","package":"b","language":"go","metrics":{"sloc":60},"base":{"sloc":40},"sloc_delta":20,"violations":[]}
{"commit":"x1","package":"<module>","language":"go","metrics":{"dup_blocks_cross_pkg":12},"base":{"dup_blocks_cross_pkg":10},"sloc_delta":0,"violations":[],"cross_added":[{"base":[125],"blocks":[{"tokens":70,"kind":"code","packages":["a","b"],"occurrences":[]},{"tokens":130,"kind":"code","packages":["a","b"],"occurrences":[]}]}]}
{"commit":"x1","package":"a","language":"go","metrics":{"sloc":60},"base":{"sloc":40},"sloc_delta":20,"violations":[]}
`
	const labelsYAML = `source: {repository: r, range: x, data: d}
commits:
  - {hash: u1, verdict: allow, reason: r, provenance: confirmed, agent: true}
  - {hash: x1, verdict: allow, reason: r, provenance: confirmed, agent: true}
`
	c, err := Load(writeReplay(t, commits, packages, labelsYAML), true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Unavailable != 1 || c.UnavailableViolations != 1 || !c.RecordsCross {
		t.Errorf("unavailable %d rows with %d violations, records cross %v; want 1, 1, true", c.Unavailable, c.UnavailableViolations, c.RecordsCross)
	}
	u := c.Commits[0]
	if len(u.Rows) != 1 || u.Rows[0].Package != "b" || u.Failed() || !u.Passed {
		t.Errorf("commit u1 rows %+v, passed %v; want only b, passed", u.Rows, u.Passed)
	}
	mod := c.Commits[1].Rows[0]
	if !mod.HasCross() || c.Commits[1].Rows[1].HasCross() {
		t.Errorf("module row records cross %v, package row %v; want true, false", mod.HasCross(), c.Commits[1].Rows[1].HasCross())
	}
	for floor, want := range map[int]int{60: 1, 80: 0, 126: 1, 131: 0} {
		if n, ok := mod.AddedCross(floor); !ok || n != want {
			t.Errorf("AddedCross(%d) = %d, %v; want %d", floor, n, ok, want)
		}
	}
	if _, ok := c.Commits[1].Rows[1].AddedCross(80); ok {
		t.Error("a package row records added cross-package blocks")
	}
	if p := Pool("all", c, c); p.Unavailable != 2 || !p.RecordsCross {
		t.Errorf("pool unavailable %d, records cross %v", p.Unavailable, p.RecordsCross)
	}
}
