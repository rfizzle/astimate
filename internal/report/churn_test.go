package report

import (
	"bytes"
	"regexp"
	"testing"
)

// TestWriteTableChurn checks that the metrics section prints the churn
// metrics as n/a when they are null, where every other null v1 metric is
// left out, and their counts when measured.
func TestWriteTableChurn(t *testing.T) {
	t.Parallel()

	commits, authors := 7, 2
	tests := []struct {
		name          string
		commits       *int
		authors       *int
		want, notWant []string
	}{
		{name: "null", want: []string{`churn_commits\s+n/a`, `churn_authors\s+n/a`}, notWant: []string{`coverage_pct`}},
		{name: "measured", commits: &commits, authors: &authors,
			want: []string{`churn_commits\s+7\b`, `churn_authors\s+2\b`}, notWant: []string{`n/a`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var r Report
			r.PackagePath = "a"
			r.Metrics.ChurnCommits, r.Metrics.ChurnAuthors = tt.commits, tt.authors
			var buf bytes.Buffer
			if err := WriteTable(&buf, &r); err != nil {
				t.Fatalf("WriteTable: %v", err)
			}
			for _, w := range tt.want {
				if !regexp.MustCompile(w).Match(buf.Bytes()) {
					t.Errorf("table does not match %q:\n%s", w, buf.String())
				}
			}
			for _, w := range tt.notWant {
				if regexp.MustCompile(w).Match(buf.Bytes()) {
					t.Errorf("table matches %q:\n%s", w, buf.String())
				}
			}
		})
	}
}
