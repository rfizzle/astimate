package corpus

import (
	"slices"
	"testing"
)

// TestLoadWarned checks that a row keeps the warn rules' breaches the
// replay recorded as warnings with severity warn, apart from its
// violations and without the other warnings, that Breached reads both, and
// that a commit whose only breach is a warn rule's passed as recorded.
func TestLoadWarned(t *testing.T) {
	t.Parallel()

	src := writeReplay(t,
		`{"commit":"c1","subject":"s","config_version":"v","baseline":"parent","loaded":true,"passed":false}
{"commit":"c2","subject":"s","config_version":"v","baseline":"parent","loaded":true,"passed":true}
`,
		`{"commit":"c1","package":"a","language":"go","metrics":{},"base":{},"violations":[{"metric":"globals"}],"warnings":[{"metric":"dup_blocks","severity":"warn"},{"metric":"sloc"}]}
{"commit":"c2","package":"a","language":"go","metrics":{},"base":{},"violations":[],"warnings":[{"metric":"dup_blocks","severity":"warn"}]}
`,
		"source: {repository: r, range: x, data: d}\ncommits:\n"+
			"  - {hash: c1, verdict: block, reason: r, provenance: proposed}\n"+
			"  - {hash: c2, verdict: allow, reason: r, provenance: proposed}\n")
	c, err := Load(src, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Commits) != 2 {
		t.Fatalf("loaded %d commits, want 2", len(c.Commits))
	}
	r := &c.Commits[0].Rows[0]
	if !slices.Equal(r.Violated, []string{"globals"}) || !slices.Equal(r.Warned, []string{"dup_blocks"}) {
		t.Errorf("violated %q warned %q, want globals and dup_blocks", r.Violated, r.Warned)
	}
	for metric, want := range map[string]bool{"globals": true, "dup_blocks": true, "sloc": false} {
		if got := r.Breached(metric); got != want {
			t.Errorf("Breached(%q) = %v, want %v", metric, got, want)
		}
	}
	if w := &c.Commits[1]; w.Failed() || !w.Passed || !w.Rows[0].Breached("dup_blocks") {
		t.Errorf("c2 failed %v passed %v, want a passed commit with the warn breach recorded", w.Failed(), w.Passed)
	}
}
