package report

import (
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/fit/internal/pool"
	"github.com/rfizzle/astimate/internal/config"
)

// synthRows returns n rows with sloc, largest_file_sloc and
// duplication_pct running 1..n and has_tests set.
func synthRows(n int) []pool.Row {
	rows := make([]pool.Row, n)
	for i := range rows {
		v := i + 1
		rows[i].Module = "example.com/m"
		rows[i].Metrics.SLOC = v
		rows[i].Metrics.LargestFileSLOC = v
		rows[i].Metrics.DuplicationPct = float64(v)
		rows[i].Metrics.HasTests = true
	}
	return rows
}

// TestOverridden checks a fitted density or capacity rule is overridden,
// and a requirement rule and an unmeasured metric are not.
func TestOverridden(t *testing.T) {
	base, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	choices := pool.FitThresholds(synthRows(100), base)
	byMetric := make(map[string]*pool.Choice, len(choices))
	for i := range choices {
		byMetric[choices[i].Rule.Metric] = &choices[i]
	}
	if c := byMetric["sloc"]; !Overridden(c) {
		t.Error("sloc: want overridden, a capacity rule the data fitted a max for")
	}
	if c := byMetric["has_tests"]; Overridden(c) {
		t.Error("has_tests: want not overridden, a requirement rule")
	}
	if c := byMetric["changed_func_cognitive_max"]; Overridden(c) {
		t.Error("changed_func_cognitive_max: want not overridden, no row measures it")
	}
}

func TestNum(t *testing.T) {
	if got := Num(1.5); got != "1.5" {
		t.Errorf("Num(1.5) = %q, want %q", got, "1.5")
	}
	if got := Num(3); got != "3" {
		t.Errorf("Num(3) = %q, want %q", got, "3")
	}
}

func TestOpt(t *testing.T) {
	if got := Opt(nil); got != "none" {
		t.Errorf("Opt(nil) = %q, want none", got)
	}
	v := 4.0
	if got := Opt(&v); got != "4" {
		t.Errorf("Opt(&4) = %q, want 4", got)
	}
}

func TestWrap(t *testing.T) {
	got := Wrap("one two three four", 8)
	want := []string{"one two", "three", "four"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("Wrap = %q, want %q", got, want)
	}
}

func TestMethodText(t *testing.T) {
	lines := MethodText()
	if len(lines) == 0 {
		t.Fatal("MethodText returned no lines")
	}
	if !strings.Contains(lines[0], "Percentiles are nearest-rank") {
		t.Errorf("MethodText()[0] = %q, want it to describe percentiles", lines[0])
	}
}

// TestRenderReportWholeConfig renders a whole-configuration candidate's
// report and checks its sections.
func TestRenderReportWholeConfig(t *testing.T) {
	base, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	choices := pool.FitThresholds(synthRows(100), base)
	in := &Input{
		Version:     "thresholds-2026-09-28",
		BaseVersion: base.Version,
		Data:        "data.jsonl",
		Candidate:   "candidate.yaml",
		Rows:        100,
		Modules:     []string{"example.com/m"},
		Choices:     choices,
		CrossPkg:    pool.CrossPkgStats(synthRows(0)),
	}
	got := RenderReport(in)
	for _, want := range []string{
		"# Threshold candidate thresholds-2026-09-28",
		"## Method",
		"## Summary",
		"## What changed most",
		"## Not fitted",
		"## Per metric",
		"### `sloc` (capacity)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report does not contain %q", want)
		}
	}
	if strings.Contains(got, "## Cross-package duplication") {
		t.Error("report has a cross-package section with no module rows")
	}
}

// TestRenderReportLanguageOverride renders a language override's report
// and checks the override-specific sections it adds.
func TestRenderReportLanguageOverride(t *testing.T) {
	base, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	rows := synthRows(100)
	for i := range rows {
		rows[i].Language = "typescript"
	}
	choices := pool.FitThresholds(rows, base)
	in := &Input{
		Version:     base.Version + "+typescript",
		BaseVersion: base.Version,
		Data:        "ts.jsonl",
		Candidate:   "override.yaml",
		Rows:        100,
		Modules:     []string{"example.com/ts"},
		Choices:     choices,
		Language:    "typescript",
	}
	got := RenderReport(in)
	for _, want := range []string{
		"# Threshold override " + in.Version,
		"## Override",
		"- Replaced for typescript:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report does not contain %q", want)
		}
	}
}

// TestRenderReportWithModuleRows checks the cross-package duplication and
// base-data sections render when the input carries module rows and base
// statistics.
func TestRenderReportWithModuleRows(t *testing.T) {
	base, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	rows := synthRows(20)
	values := []int{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	modRows := make([]pool.Row, len(values))
	for i, v := range values {
		modRows[i] = pool.Row{Module: "example.com/m" + strconv.Itoa(i), Package: "<module>"}
		modRows[i].Metrics.DupBlocksCrossPkg = &v
	}
	rows = append(rows, modRows...)
	choices := pool.FitThresholds(rows, base)
	in := &Input{
		Version:     "thresholds-2026-09-28",
		BaseVersion: base.Version,
		Data:        "data.jsonl",
		Candidate:   "candidate.yaml",
		Rows:        20,
		ModuleRows:  len(values),
		ModulesData: "modules.jsonl",
		Modules:     []string{"example.com/m"},
		Choices:     choices,
		CrossPkg:    pool.CrossPkgStats(rows),
	}
	got := RenderReport(in)
	for _, want := range []string{
		"## Cross-package duplication",
		"- Module rows: `modules.jsonl`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report does not contain %q", want)
		}
	}
}
