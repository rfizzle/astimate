package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/report"
)

// TestPackageRowBaselineFrom checks that a row whose baseline was seeded
// from renames records the sources and is not new, and that any other row
// leaves the field out.
func TestPackageRowBaselineFrom(t *testing.T) {
	t.Parallel()

	seeded := newPackageRow("c", &report.Report{
		PackagePath: "agent/sidejobs", Baseline: &report.Baseline{}, BaselineFrom: []string{"agent"},
	})
	if seeded.New || !slices.Equal(seeded.BaselineFrom, []string{"agent"}) {
		t.Errorf("seeded row new %v, baseline_from %q, want not new and [agent]", seeded.New, seeded.BaselineFrom)
	}
	data, err := json.Marshal(seeded)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"baseline_from":["agent"]`) {
		t.Errorf("seeded row JSON lacks baseline_from: %s", data)
	}

	plain := newPackageRow("c", &report.Report{PackagePath: "a"})
	if data, err = json.Marshal(plain); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "baseline_from") {
		t.Errorf("new row JSON has baseline_from: %s", data)
	}
}
