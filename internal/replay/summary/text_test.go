package summary

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestWriteText checks the text summary's lines; cmd/astimate's
// TestDryrunSummaryGolden pins the whole of it.
func TestWriteText(t *testing.T) {
	t.Parallel()

	s := &Summary{Commits: 3, Checked: 2, Touched: 2, Failed: 1,
		NotLoaded: []NotLoaded{{Commit: "0123456789abcdef", Subject: "wip", Error: "a\n  b"}},
		Rules:     []RuleCount{{Metric: "sloc", Failed: 1, Alone: 1}},
		Packages:  []PackageCount{{Package: "x", Failed: 1}}}
	var buf bytes.Buffer
	if err := WriteText(&buf, s, Header{Selected: 4, Budget: 0.1, Wall: 1500 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"replayed 3 of 4 commits in 2s: 2 checked, 1 not loaded, 2 touched a package\n",
		"would have failed 1 of 2 commits checked (50.0%), 1 of the 2 that touched a package (50.0%)\n",
		"sloc       1   50.0%      1  over budget\n",
		"budget 10.0% of the commits checked",
		"  x  1 commit\n",
		"  0123456789ab wip: a b\n",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("summary lacks %q:\n%s", want, buf.String())
		}
	}
}
