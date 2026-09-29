package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/report"
)

// TestFindingsSeverity checks that a row keeps a warn rule's severity on
// its warning, so calibration/validate can tell the breach from a capacity
// warning, and leaves it out of every other finding.
func TestFindingsSeverity(t *testing.T) {
	t.Parallel()

	fs := findings([]report.Finding{
		{Metric: "globals", Head: 1, Limit: "max_delta +0", Suggestion: "dropped", Severity: "warn"},
		{Metric: "sloc", Head: 800, Limit: "max 1000"},
	})
	data, err := json.Marshal(fs)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	want := `[{"metric":"globals","base":null,"head":1,"limit":"max_delta +0","location":null,"severity":"warn"},` +
		`{"metric":"sloc","base":null,"head":800,"limit":"max 1000","location":null}]`
	if got != want {
		t.Errorf("findings = %s, want %s", got, want)
	}
	if strings.Contains(got, "dropped") {
		t.Errorf("findings keep the suggestion: %s", got)
	}
}
