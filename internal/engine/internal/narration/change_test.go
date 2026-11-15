package narration

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/report"
)

// failing returns a check of n packages, each failing one rule.
func failing(n int) *report.Check {
	c := &report.Check{Packages: make([]report.CheckedPackage, 0, n)}
	for i := range n {
		r := pkg("p"+strconv.Itoa(i), 100+i)
		r.Violations = []report.Finding{{Metric: "sloc", Head: float64(100 + i), Limit: "max 99", Suggestion: "Split it."}}
		c.Packages = append(c.Packages, report.CheckedPackage{Report: r})
	}
	return c
}

// TestWriteChange writes the change block as Markdown, or as annotations
// with the Markdown appended to the step summary, naming the findings the
// cap left out in both.
func TestWriteChange(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name          string
		n             int
		github        bool
		summary       bool
		want, notWant []string
		wantSummary   []string
	}{
		{name: "markdown", n: 1, want: []string{"## astimate: FAIL", "| `p0` | sloc |"}, notWant: []string{"::error"}},
		{name: "github", n: 1, github: true, want: []string{"::error file=p0"}, notWant: []string{"## astimate"}},
		{
			name: "github with summary", n: 1, github: true, summary: true,
			want: []string{"::error file=p0"}, wantSummary: []string{"## astimate: FAIL"},
		},
		{
			name: "github over the cap", n: report.MaxAnnotations + 2, github: true, summary: true,
			want:        []string{"::error file=p", "astimate: 2 more findings are not annotated"},
			wantSummary: []string{"## astimate: FAIL", "2 more findings are not annotated; the report lists them all.\n"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := ChangeOptions{GitHub: tt.github}
			if tt.summary {
				opts.SummaryFile = filepath.Join(t.TempDir(), "summary.md")
			}
			var b strings.Builder
			if err := WriteChange(module(t), failing(tt.n), opts, &b); err != nil {
				t.Fatalf("WriteChange: %v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(b.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, b.String())
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(b.String(), notWant) {
					t.Errorf("output has %q:\n%s", notWant, b.String())
				}
			}
			if !tt.summary {
				return
			}
			data, err := os.ReadFile(opts.SummaryFile)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.wantSummary {
				if !strings.Contains(string(data), want) {
					t.Errorf("summary lacks %q:\n%s", want, data)
				}
			}
		})
	}
}

// TestAppendSummary appends to a step summary that already has content and
// says how many findings the cap left without an annotation.
func TestAppendSummary(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "summary.md")
	if err := os.WriteFile(path, []byte("earlier step\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &narrate.Module{Change: &narrate.Change{Verdict: "FAIL", Changed: 25, Failed: 25, Violations: 25}}
	note := report.OmittedText(report.Omitted{Findings: 15})
	if err := appendSummary(path, m, note); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"earlier step\n## astimate: FAIL — 25 of 25 changed packages need attention\n",
		"\n15 more findings are not annotated; the report lists them all.\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary lacks %q:\n%s", want, got)
		}
	}
}
