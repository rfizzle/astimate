package narrate

import (
	"testing"

	"github.com/rfizzle/astimate/internal/report"
)

func TestHookNotes(t *testing.T) {
	t.Parallel()

	m := &Module{Change: &Change{Failing: []Failing{{Path: "app", Findings: []Finding{
		{Metric: "dup_blocks", Why: "a copy", Location: &report.Location{File: "app/a.go", Line: 15}},
		{Metric: "globals", Location: &report.Location{File: "app/b.go"}},
		{Metric: "sloc", Why: "too big"},
	}}}}}
	tests := []struct {
		path, metric, why, open string
	}{
		{"app", "dup_blocks", "a copy", "app/a.go:15"},
		{"app", "globals", "", "app/b.go"},
		{"app", "sloc", "too big", ""},
		{"app", "untested_exports", "", ""},
		{"lib", "dup_blocks", "", ""},
	}
	notes := m.HookNotes()
	for _, tt := range tests {
		if why, open := notes(tt.path, tt.metric); why != tt.why || open != tt.open {
			t.Errorf("notes(%q, %q) = %q, %q, want %q, %q", tt.path, tt.metric, why, open, tt.why, tt.open)
		}
	}
	if why, open := (&Module{}).HookNotes()("app", "dup_blocks"); why != "" || open != "" {
		t.Errorf("notes without a change = %q, %q, want none", why, open)
	}
}
