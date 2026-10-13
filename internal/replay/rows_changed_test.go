package replay

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

// TestPackageRowChangedFunctions checks a row records its changed
// functions over the floor with their head and parent complexity, an
// empty list when none is over it, and null without a function-level
// diff, and that a finding keeps its also list.
func TestPackageRowChangedFunctions(t *testing.T) {
	t.Parallel()

	worst := 369
	fns := []metrics.ChangedFunction{
		{FunctionInfo: metrics.FunctionInfo{Receiver: "UI", Name: "Update", File: "ui.go", Line: 20, Cognitive: 369},
			BaseCognitive: 369, HasBase: true},
		{FunctionInfo: metrics.FunctionInfo{Name: "small", File: "s.go", Line: 3, Cognitive: 20}},
		{FunctionInfo: metrics.FunctionInfo{Name: "grade", File: "g.go", Line: 8, Cognitive: 51}},
	}
	p := &report.CheckedPackage{
		Report: report.Report{
			PackagePath: "a", Metrics: metrics.RawMetrics{ChangedFuncCognitiveMax: &worst},
			Violations: []report.Finding{{Metric: "dup_blocks", Head: 1, Limit: "max_delta +0", Also: []string{"duplication_pct"}}},
		},
		ChangedFunctions: fns,
	}
	data, err := json.Marshal(newPackageRow("c", p))
	if err != nil {
		t.Fatal(err)
	}
	want := `"changed_functions":[{"receiver":"UI","name":"Update","file":"ui.go","line":20,"cognitive":369,"base":369},` +
		`{"name":"grade","file":"g.go","line":8,"cognitive":51,"base":null}]`
	if !strings.Contains(string(data), want) {
		t.Errorf("row JSON = %s, want it to contain %s", data, want)
	}
	if !strings.Contains(string(data), `"also":["duplication_pct"]`) {
		t.Errorf("row JSON = %s, want the finding's also list", data)
	}

	zero := 0
	none := &report.CheckedPackage{Report: report.Report{PackagePath: "a", Metrics: metrics.RawMetrics{ChangedFuncCognitiveMax: &zero}}}
	if data, err = json.Marshal(newPackageRow("c", none)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"changed_functions":[]`) {
		t.Errorf("row with no changed function JSON = %s, want an empty list", data)
	}

	module := &report.CheckedPackage{Report: report.Report{PackagePath: metrics.ModuleRowID}}
	if data, err = json.Marshal(newPackageRow("c", module)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"changed_functions":null`) {
		t.Errorf("row without a function diff JSON = %s, want null", data)
	}
}
