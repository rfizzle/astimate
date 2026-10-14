package markdown

import (
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/report"
)

func ptrTo(v float64) *float64 { return &v }

// TestWriteChange lays out the change block for a failing change, a clean
// one and an empty one.
func TestWriteChange(t *testing.T) {
	t.Parallel()

	failing := &narrate.Change{
		Verdict: "FAIL", Changed: 5, Failed: 2, Violations: 3, Warnings: 3, Passed: 2,
		Cross: &narrate.Cross{Base: ptrTo(2), Head: 4},
		Failing: []narrate.Failing{
			{Path: "internal/app", Findings: []narrate.Finding{
				{Label: "sloc", Base: ptrTo(1200), Head: 1534.5, Location: &report.Location{File: "internal/app/a.go", Line: 12}},
				{Label: "dup blocks", Base: ptrTo(1), Head: 3},
			}},
			{Path: "<module>", Findings: []narrate.Finding{{Label: "dup blocks cross pkg", Head: 4, Location: &report.Location{File: "x|y.go"}}}},
		},
		WarningGroups: []narrate.WarningGroup{{Label: "tokens est"}, {Label: "globals"}},
	}
	tests := []struct {
		name   string
		change *narrate.Change
		opts   Options
		want   string
	}{
		{"failing with the artifact", failing, Options{Artifact: "astimate-report.html"}, "" +
			"## astimate: FAIL — 2 of 5 changed packages need attention\n\n" +
			"| Package | What moved | Change | Open |\n|---|---|---|---|\n" +
			"| `internal/app` | sloc | 1,200 → 1,534.5 | `internal/app/a.go:12` |\n" +
			"| `internal/app` | dup blocks | 1 → 3 |  |\n" +
			"| `<module>` | dup blocks cross pkg | new → 4 | `x\\|y.go` |\n\n" +
			"3 heads-ups (tokens est, globals), 2 packages pass clean. Copies between packages 2 → 4.\n" +
			"Full report: `astimate-report.html` (CI artifact).\n"},
		{"clean", &narrate.Change{Verdict: "PASS", Changed: 1, Passed: 1}, Options{}, "" +
			"## astimate: PASS — 1 changed package, nothing needs attention\n\n1 package passes clean.\n"},
		{"empty", &narrate.Change{Verdict: "PASS"}, Options{}, "## astimate: PASS — no package changed\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var b strings.Builder
			if err := WriteChange(&b, &narrate.Module{Change: tt.change}, tt.opts); err != nil {
				t.Fatal(err)
			}
			if b.String() != tt.want {
				t.Errorf("markdown =\n%s\nwant\n%s", b.String(), tt.want)
			}
		})
	}
}

// TestWriteChangeNone reports a module without a change section.
func TestWriteChangeNone(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	if err := WriteChange(&b, &narrate.Module{}, Options{}); err == nil || b.Len() != 0 {
		t.Errorf("WriteChange without a change = %v, %q, want an error and no output", err, b.String())
	}
}

// TestWriteRepository lists the three packages to start with and ten rows,
// and puts the change block first when there is one.
func TestWriteRepository(t *testing.T) {
	t.Parallel()

	m := &narrate.Module{Headline: "Two packages need a split.", Lead: "It has 12 packages.", Hotspots: []string{"p3", "p1", "gone"}}
	for _, p := range []string{"p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11"} {
		pkg := narrate.Package{Path: p, Band: narrate.BandFine, Story: "story " + p, Todo: "todo " + p}
		pkg.Rebuild.HumanDays = float64(len(p))
		pkg.Rebuild.HumanShort = "1 d"
		m.Packages = append(m.Packages, pkg)
	}
	m.Packages[3].Churn = &narrate.Churn{Commits: 7}
	m.Packages[3].Over = []string{"sloc"}
	m.Packages[3].Copies.Lines = 1500
	m.Change = &narrate.Change{Verdict: "PASS"}

	var b strings.Builder
	if err := Write(&b, m, Options{}); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{
		"## astimate: PASS — no package changed\n\n## Two packages need a split.\n\nIt has 12 packages.\n",
		"1. `p3` (fine): 1 d to rebuild, 7 changes\n   story p3\n   Do this: todo p3\n",
		"2. `p1` (fine): 1 d to rebuild\n",
		"| `p3` | fine | 1 d | 1,500* | 0% | 0 | 7 |\n",
		"… 2 more packages;",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("markdown lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "gone") || strings.Count(got, "\n| `") != 10 {
		t.Errorf("markdown lists an unknown hotspot or not ten rows:\n%s", got)
	}
}
