package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/report/details"
)

// wantDupesFunctions are the functions of the fixture's dupes package with
// the cyclomatic complexity gocyclo gives each (testdata/go/fixture/golden/COUNTING.md).
func wantDupesFunctions() []details.Function {
	return []details.Function{
		{Name: "SumOrders", File: "dupes.go", Line: 9, Cyclomatic: 5},
		{Name: "TallyScores", File: "dupes.go", Line: 31, Cyclomatic: 5},
		{Name: "CountVisits", File: "dupes.go", Line: 53, Cyclomatic: 5},
		{Name: "Describe", File: "dupes.go", Line: 73, Cyclomatic: 3},
	}
}

// checkFunctions checks that r's details list the dupes package's
// functions with their cyclomatic complexity, and that the per-function
// values add up to the package's cognitive_total and cyclomatic_total.
func checkFunctions(t *testing.T, r *report.Report) {
	t.Helper()
	if r.Details == nil {
		t.Fatalf("report of %s has no details", r.PackagePath)
	}
	got := slices.Clone(r.Details.Functions)
	cognitive, cyclomatic := 0, 0
	for i := range got {
		cognitive += got[i].Cognitive
		cyclomatic += got[i].Cyclomatic
		got[i].Cognitive = 0
	}
	if want := wantDupesFunctions(); !slices.Equal(got, want) {
		t.Errorf("details.functions = %+v, want %+v (cognitive aside)", r.Details.Functions, want)
	}
	if cognitive != r.Metrics.CognitiveTotal || cyclomatic != r.Metrics.CyclomaticTotal {
		t.Errorf("per-function sums cognitive %d, cyclomatic %d; want cognitive_total %d, cyclomatic_total %d",
			cognitive, cyclomatic, r.Metrics.CognitiveTotal, r.Metrics.CyclomaticTotal)
	}
	if r.Metrics.CyclomaticTotal != 18 || r.Metrics.CyclomaticP90 != 5 {
		t.Errorf("cyclomatic_total, cyclomatic_p90 = %d, %d, want 18, 5", r.Metrics.CyclomaticTotal, r.Metrics.CyclomaticP90)
	}
}

// TestAssessJSONFunctions checks that assess --json carries the cyclomatic
// metrics and the per-function values behind them in details.functions.
func TestAssessJSONFunctions(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	args := []string{"assess", "--json", filepath.Join(fixtureDir, "dupes")}
	if got := run(args, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, stderr.String())
	}
	var r report.Report
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("decoding report: %v\n%s", err, stdout.String())
	}
	checkFunctions(t, &r)
}

// TestCheckJSONFunctions checks that baseline write records each
// function's cyclomatic complexity and that check --format json carries the
// cyclomatic metrics and details.functions on every checked package.
func TestCheckJSONFunctions(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	base := filepath.Join(t.TempDir(), "baseline.json")
	var stdout, stderr bytes.Buffer
	args := []string{"baseline", "write", fixtureDir, "--out", base}
	if got := run(args, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, stderr.String())
	}
	raw, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Functions map[string][]struct {
			Name       string `json:"name"`
			Cyclomatic int    `json:"cyclomatic"`
		} `json:"functions"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decoding the baseline file: %v", err)
	}
	var recorded []int
	for _, f := range file.Functions["example.com/fixture/dupes"] {
		recorded = append(recorded, f.Cyclomatic)
	}
	if want := []int{5, 5, 5, 3}; !slices.Equal(recorded, want) {
		t.Errorf("baseline file dupes cyclomatic = %v, want %v", recorded, want)
	}

	stdout.Reset()
	stderr.Reset()
	args = []string{"check", fixtureDir, "--all", "--config", pinnedConfigFile(t), "--baseline", base, "--format", formatJSON}
	if got := run(args, &stdout, &stderr); got != exitOK && got != exitGateFailed {
		t.Fatalf("run(%q) exit code = %d; stderr:\n%s", args, got, stderr.String())
	}
	for _, r := range decodeReports(t, stdout.Bytes()) {
		if r.PackagePath == "dupes" {
			checkFunctions(t, &r)
			return
		}
	}
	t.Fatalf("check reports have no dupes row:\n%s", stdout.String())
}
