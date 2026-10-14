package replay

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

// TestPackageRowCyclomatic checks a row carries cyclomatic_total and
// cyclomatic_p90 in its head and parent metrics, so a validation can read
// them from the next replay on.
func TestPackageRowCyclomatic(t *testing.T) {
	t.Parallel()

	p := &report.CheckedPackage{Report: report.Report{
		PackagePath: "a",
		Metrics:     metrics.RawMetrics{CyclomaticTotal: 42, CyclomaticP90: 7},
		Baseline:    &report.Baseline{Metrics: metrics.RawMetrics{CyclomaticTotal: 40, CyclomaticP90: 6}},
	}}
	data, err := json.Marshal(newPackageRow("c", p))
	if err != nil {
		t.Fatal(err)
	}
	var row struct {
		Metrics map[string]json.RawMessage `json:"metrics"`
		Base    map[string]json.RawMessage `json:"base"`
	}
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		what string
		m    map[string]json.RawMessage
		want [2]string
	}{
		{"metrics", row.Metrics, [2]string{"42", "7"}},
		{"base", row.Base, [2]string{"40", "6"}},
	} {
		if got := [2]string{string(c.m["cyclomatic_total"]), string(c.m["cyclomatic_p90"])}; got != c.want {
			t.Errorf("row %s cyclomatic_total, cyclomatic_p90 = %v, want %v in %s", c.what, got, c.want, data)
		}
	}
	if !strings.Contains(string(data), `"cognitive_p90":0,"cyclomatic_total":42,"cyclomatic_p90":7,"func_count"`) {
		t.Errorf("row JSON = %s, want the cyclomatic fields after cognitive_p90", data)
	}
}
