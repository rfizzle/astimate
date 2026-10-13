package engine

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/report"
)

// foldSource returns two copies of a long loop, a legacy duplicate block,
// as the exported functions Legacy1 and Legacy2, followed by extra.
func foldSource(extra string) string {
	var b strings.Builder
	b.WriteString("package p\n")
	for _, n := range []string{"1", "2"} {
		b.WriteString("\n// Legacy" + n + " folds xs.\nfunc Legacy" + n + "(xs []int) int {\n\ttotal := 0\n" +
			"\tfor i, x := range xs {\n\t\tif x > i {\n\t\t\ttotal += x * 2\n\t\t} else {\n\t\t\ttotal -= x\n\t\t}\n" +
			"\t\tif total > 100 {\n\t\t\ttotal %= 7\n\t\t}\n\t}\n\treturn total\n}\n")
	}
	return b.String() + extra
}

// joinSource returns two copies of a shorter loop, the duplicate block a
// change adds, as the exported functions Zeta1 and Zeta2.
func joinSource() string {
	var b strings.Builder
	b.WriteString("package p\n\nimport \"strings\"\n")
	for _, n := range []string{"1", "2"} {
		b.WriteString("\n// Zeta" + n + " joins s.\nfunc Zeta" + n + "(s []string) string {\n" +
			"\tout := make([]string, 0, len(s))\n\tfor _, e := range s {\n\t\tout = append(out, e+\"!\")\n\t}\n" +
			"\treturn strings.Join(out, \",\")\n}\n")
	}
	return b.String()
}

// TestCheckCitesAddedBlock checks a change that adds a duplicate block and
// two untested exports to a package that already had a longer duplicate
// block, earlier in file order, and two untested exports that sort first.
// The change also edits the legacy block's file, so preferring a changed
// file would not tell the blocks apart. Each finding cites, and is located
// on, what the change added, the duplicate finding saying so, and the text
// and GitHub formats match their goldens.
func TestCheckCitesAddedBlock(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	dir := gitRepoAt(t, map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.22\n",
		"p/a.go": foldSource(""),
	})
	writeFiles(t, dir, map[string]string{
		"p/a.go": foldSource("\nfunc bump(x int) int { return x + 1 }\n"),
		"p/z.go": joinSource(),
	})
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "--no-verify", "-m", "add")

	c, logs := checkAgainstBase(t, dir, false)
	p := findRow(c, "p")
	if p == nil {
		t.Fatalf("checked %d packages, want p; logs:\n%s", len(c.Packages), logs)
	}
	if p.Details == nil || len(p.Details.Duplicates) != 4 || p.Details.Duplicates[0].File != "z.go" ||
		p.Details.Duplicates[2].File != "a.go" {
		t.Fatalf("details = %+v, want the added block's two occurrences before the legacy block's", p.Details)
	}
	want := map[string]struct{ text, loc string }{
		"dup_blocks":       {"starting with z.go:6-12, added in this change.", "p/z.go:6"},
		"untested_exports": {"(Zeta1, Zeta2, Legacy1, Legacy2)", "p/z.go:6"},
	}
	for _, f := range p.Violations {
		w, ok := want[f.Metric]
		if !ok {
			continue
		}
		delete(want, f.Metric)
		if !strings.Contains(f.Suggestion, w.text) {
			t.Errorf("%s suggestion %q, want it to contain %q", f.Metric, f.Suggestion, w.text)
		}
		if f.Location == nil || f.Location.File+":"+strconv.Itoa(f.Location.Line) != w.loc {
			t.Errorf("%s located at %+v, want %s", f.Metric, f.Location, w.loc)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing violations %v in %+v", want, p.Violations)
	}

	var text, github bytes.Buffer
	if err := report.WriteCheckText(&text, c); err != nil {
		t.Fatal(err)
	}
	if err := report.WriteGitHub(&github, c); err != nil {
		t.Fatal(err)
	}
	compareGolden(t, filepath.Join("testdata", "added", "text.golden"), text.String())
	compareGolden(t, filepath.Join("testdata", "added", "github.golden"), github.String())
}
