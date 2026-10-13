package corpus

import "testing"

// TestLoadChangedFunctions checks a row's recorded changed functions are
// read with their parent complexity, null for a new function, that an
// empty list still counts as recorded, and that a row without the field,
// as one replayed before it existed, records none.
func TestLoadChangedFunctions(t *testing.T) {
	t.Parallel()

	src := writeReplay(t,
		`{"commit":"c1","subject":"s","config_version":"v","baseline":"parent","loaded":true,"passed":true}
`,
		`{"commit":"c1","package":"a","language":"go","metrics":{},"base":{},"violations":[],"warnings":[],"changed_functions":[{"receiver":"UI","name":"Update","cognitive":385,"base":369},{"name":"grade","cognitive":51,"base":null}]}
{"commit":"c1","package":"b","language":"go","metrics":{},"base":{},"violations":[],"warnings":[],"changed_functions":[]}
{"commit":"c1","package":"c","language":"go","metrics":{},"base":{},"violations":[],"warnings":[]}
`,
		"source: {repository: r, range: x, data: d}\ncommits:\n"+
			"  - {hash: c1, verdict: block, reason: r, provenance: proposed}\n")
	c, err := Load(src, false)
	if err != nil {
		t.Fatal(err)
	}
	rows := c.Commits[0].Rows
	a := rows[0].Changed
	if !rows[0].HasChanged() || len(a) != 2 {
		t.Fatalf("row a changed = %+v, want two functions", a)
	}
	if f := a[0]; f.QualifiedName() != "UI.Update" || f.Cognitive != 385 || !f.HasBase || f.BaseCognitive != 369 {
		t.Errorf("row a function 0 = %+v, want UI.Update 369 -> 385", f)
	}
	if f := a[1]; f.Name != "grade" || f.Cognitive != 51 || f.HasBase {
		t.Errorf("row a function 1 = %+v, want new grade at 51", f)
	}
	if !rows[1].HasChanged() || len(rows[1].Changed) != 0 {
		t.Errorf("row b = %+v, want an empty recorded list", rows[1].Changed)
	}
	if rows[2].HasChanged() {
		t.Errorf("row c = %+v, want none recorded", rows[2].Changed)
	}
}
