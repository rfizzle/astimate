package narrate

import (
	"testing"

	"github.com/rfizzle/astimate/internal/report"
)

func TestLookup(t *testing.T) {
	t.Parallel()

	terms := Glossary("go", []Limit{{Metric: "sloc", Max: 1000}}, 60, 200, report.HumanBasis{})
	tests := []struct {
		name, want string
		found      bool
	}{
		{"rebuild_cost", "Rebuild cost", true},
		{"Rebuild cost", "Rebuild cost", true},
		{"hard-to-follow", "Hard to follow", true},
		{"PATHS", "Hard to follow", true},
		{"limits", "Limits", true},
		{"dup_blocks", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := Lookup(terms, tt.name)
			if ok != tt.found || got.Term != tt.want {
				t.Errorf("Lookup(%q) = %q, %v, want %q, %v", tt.name, got.Term, ok, tt.want, tt.found)
			}
		})
	}
}
