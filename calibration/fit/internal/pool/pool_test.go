package pool

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/metrics"
)

// optStr formats an optional limit the way the report package does,
// "none" when absent, for readable test failure messages.
func optStr(v *float64) string {
	if v == nil {
		return "none"
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

// synthRows returns n rows with sloc, largest_file_sloc and
// duplication_pct running 1..n and has_tests set.
func synthRows(n int) []Row {
	rows := make([]Row, n)
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

// moduleRows returns one module row (package metrics.ModuleRowID) per
// value, carrying it as dup_blocks_cross_pkg.
func moduleRows(values ...int) []Row {
	rows := make([]Row, len(values))
	for i, v := range values {
		rows[i] = Row{Module: "example.com/m" + strconv.Itoa(i), Package: metrics.ModuleRowID}
		rows[i].Metrics.DupBlocksCrossPkg = &v
	}
	return rows
}

func TestFitThresholds(t *testing.T) {
	base, err := config.Load("../../../../configs/uncalibrated.yaml")
	if err != nil {
		t.Fatal(err)
	}
	choices := FitThresholds(synthRows(100), base)
	byMetric := make(map[string]Choice, len(choices))
	for _, c := range choices {
		byMetric[c.Rule.Metric] = c
	}
	tests := []struct {
		metric        string
		max, maxDelta string
	}{
		{"sloc", "90", "none"},              // capacity: p90 90
		{"largest_file_sloc", "90", "none"}, // capacity
		{"duplication_pct", "90", "12.5"},   // density with a max: IQR 50 / 4 = 12.5
		{"globals", "none", "0"},            // density without a max stays without; base delta 0 is pinned
		{"has_tests", "none", "none"},       // requirement untouched
		{"tokens_est", "1", "none"},         // all zero: a capacity max is at least one step
		// No row counts its functions: the base max stays, and a max-only
		// density rule gets no max_delta.
		{"changed_func_cognitive_max", "30", "none"},
	}
	for _, tt := range tests {
		c, ok := byMetric[tt.metric]
		if !ok {
			t.Errorf("%s: not fitted", tt.metric)
			continue
		}
		if got := optStr(c.Max); got != tt.max {
			t.Errorf("%s: max = %s, want %s", tt.metric, got, tt.max)
		}
		if got := optStr(c.MaxDelta); got != tt.maxDelta {
			t.Errorf("%s: max_delta = %s, want %s", tt.metric, got, tt.maxDelta)
		}
	}
	if c := byMetric["sloc"]; c.OverCandidate != 10 {
		t.Errorf("sloc fails %d rows as new under the candidate, want the 10 above p90", c.OverCandidate)
	}
}

// TestFitFunctionCognitive checks changed_func_cognitive_max is fitted at
// the 99th percentile of the functions pooled from every row's counts,
// std rows included, and counts the functions and packages above it.
func TestFitFunctionCognitive(t *testing.T) {
	base, err := config.Load("../../../../configs/uncalibrated.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// 200 rows of one function each, cognitive 1..200, half of them std;
	// p99 is 198, which rounds to 200. A 201st row has no functions.
	rows := synthRows(201)
	for i := range 200 {
		rows[i].FuncCognitive = map[int]int{i + 1: 1}
		if i%2 == 0 {
			rows[i].Module = StdModule
		}
	}
	var c *Choice
	for _, ch := range FitThresholds(rows, base) {
		if ch.Rule.Metric == FuncMetric {
			c = &ch
		}
	}
	if c == nil {
		t.Fatal("changed_func_cognitive_max not fitted")
	}
	if c.Pool != PoolFunctions || c.Stats.N != 200 || c.Stats.P90 != 180 || c.Stats.P99 != 198 {
		t.Errorf("pool %q n %d p90 %v p99 %v, want 200 functions of all rows, p90 180, p99 198",
			c.Pool, c.Stats.N, c.Stats.P90, c.Stats.P99)
	}
	if optStr(c.Max) != "200" || optStr(c.MaxDelta) != "none" {
		t.Errorf("max %s max_delta %s, want p99 rounded to 200 and no max_delta", optStr(c.Max), optStr(c.MaxDelta))
	}
	// Base max 30: 170 functions above it, one per package.
	if c.OverBase != 170 || c.OverCandidate != 0 || c.Packages != 200 || c.PkgOverBase != 170 || c.PkgOverCandidate != 0 {
		t.Errorf("over base %d candidate %d, packages %d over base %d candidate %d; want 170, 0, 200, 170, 0",
			c.OverBase, c.OverCandidate, c.Packages, c.PkgOverBase, c.PkgOverCandidate)
	}
}

// TestFitKeepsZeroDelta checks the zero-tolerance policy of SPEC.md 11.1: a
// density rule whose base max_delta is 0 keeps 0 however wide the data's
// IQR, while a rule with a non-zero base max_delta is refitted.
func TestFitKeepsZeroDelta(t *testing.T) {
	base, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	rows := synthRows(100)
	for i := range rows {
		v := i + 1
		rows[i].Metrics.DupBlocks = v
		rows[i].Metrics.UntestedExports = v
		rows[i].Metrics.Globals = v
		rows[i].Metrics.InitFuncs = v
		rows[i].Metrics.MaxNesting = v
	}
	rows = append(rows, moduleRows(1, 50, 100)...)
	pinned := 0
	for _, c := range FitThresholds(rows, base) {
		if c.Rule.MaxDelta == nil {
			continue
		}
		switch {
		case *c.Rule.MaxDelta == 0:
			pinned++
			if c.MaxDelta == nil || *c.MaxDelta != 0 || !c.DeltaPinned {
				t.Errorf("%s: max_delta = %s (pinned %t) from IQR %v, want 0 kept", c.Rule.Metric, optStr(c.MaxDelta), c.DeltaPinned, c.Stats.IQR)
			}
		case c.DeltaPinned:
			t.Errorf("%s: base max_delta %v marked pinned", c.Rule.Metric, *c.Rule.MaxDelta)
		}
	}
	if pinned != 6 {
		t.Errorf("%d rules pinned, want the 6 zero-tolerance rules of the default", pinned)
	}
}

// TestFitCrossPkgFromModuleRows checks dup_blocks_cross_pkg, a module-wide
// metric, is fitted from the module rows alone, ignoring the per-package
// counts package rows carry: max at the p90 of the module rows, max_delta
// 0 kept by policy, and fail-as-new counted over modules.
func TestFitCrossPkgFromModuleRows(t *testing.T) {
	base, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	rows := synthRows(20)
	for i := range rows {
		big := 1000
		rows[i].Metrics.DupBlocksCrossPkg = &big
	}
	// Ten modules with 10, 20, ..., 100 blocks: p90 is 90.
	rows = append(rows, moduleRows(10, 20, 30, 40, 50, 60, 70, 80, 90, 100)...)
	var c *Choice
	for _, ch := range FitThresholds(rows, base) {
		switch ch.Rule.Metric {
		case "dup_blocks_cross_pkg":
			c = &ch
		case "sloc":
			if ch.Stats.N != 20 {
				t.Errorf("sloc pooled %d rows, want the 20 package rows only", ch.Stats.N)
			}
		}
	}
	if c == nil {
		t.Fatal("dup_blocks_cross_pkg not fitted")
	}
	if c.Pool != PoolModule || c.Stats.N != 10 || c.Stats.P90 != 90 {
		t.Errorf("pool %q n %d p90 %v, want 10 module rows and p90 90", c.Pool, c.Stats.N, c.Stats.P90)
	}
	if optStr(c.Max) != "90" || optStr(c.MaxDelta) != "0" || !c.DeltaPinned {
		t.Errorf("max %s max_delta %s pinned %t, want 90, 0 and pinned", optStr(c.Max), optStr(c.MaxDelta), c.DeltaPinned)
	}
	if c.OverCandidate != 1 {
		t.Errorf("%d modules fail the candidate as new, want the one above 90", c.OverCandidate)
	}
}

// TestFitPoolsInternalImportsFromClonedModules checks internal_imports is
// fitted from non-std rows only and every other metric from all rows.
func TestFitPoolsInternalImportsFromClonedModules(t *testing.T) {
	base, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	rows := synthRows(20)
	for i := range rows {
		rows[i].Metrics.InternalImports = 3
		if i < 10 {
			// std counts every standard-library import as internal.
			rows[i].Module = "std"
			rows[i].Metrics.InternalImports = 40
		}
	}
	for _, c := range FitThresholds(rows, base) {
		switch c.Rule.Metric {
		case "internal_imports":
			if c.Pool != PoolCloned || c.Stats.N != 10 || c.Stats.Max != 3 || optStr(c.Max) != "3" {
				t.Errorf("internal_imports: pool %q n %d max %v candidate %s, want the 10 cloned rows and max 3",
					c.Pool, c.Stats.N, c.Stats.Max, optStr(c.Max))
			}
		case "sloc":
			if c.Pool != PoolAll || c.Stats.N != 20 {
				t.Errorf("sloc: pool %q n %d, want all 20 rows", c.Pool, c.Stats.N)
			}
		}
	}
}

// TestValues checks the pooling rules of poolFor via Values
// directly: internal_imports over cloned rows, an ordinary metric over
// every row, and a module-wide metric over module rows.
func TestValues(t *testing.T) {
	rows := synthRows(5)
	rows[0].Module = StdModule
	rows = append(rows, moduleRows(7, 9)...)
	if got := Values(rows, "sloc", PoolAll); len(got) != 5 {
		t.Errorf("Values(sloc, PoolAll) has %d values, want 5", len(got))
	}
	if got := Values(rows, "sloc", PoolCloned); len(got) != 4 {
		t.Errorf("Values(sloc, PoolCloned) has %d values, want 4 (std excluded)", len(got))
	}
	if got := Values(rows, "dup_blocks_cross_pkg", PoolModule); len(got) != 2 {
		t.Errorf("Values(dup_blocks_cross_pkg, PoolModule) has %d values, want 2", len(got))
	}
}

// TestCrossPkgStats checks CrossPkgStats pools dup_blocks_cross_pkg from
// the module rows alone.
func TestCrossPkgStats(t *testing.T) {
	rows := synthRows(5)
	rows = append(rows, moduleRows(10, 20, 30)...)
	s := CrossPkgStats(rows)
	if s.N != 3 || s.P50 != 20 {
		t.Errorf("CrossPkgStats = %+v, want n 3 p50 20", s)
	}
	if s2 := CrossPkgStats(synthRows(5)); s2.N != 0 {
		t.Errorf("CrossPkgStats with no module rows has n %d, want 0", s2.N)
	}
}

// TestCountModuleRows checks CountModuleRows counts only module rows.
func TestCountModuleRows(t *testing.T) {
	rows := append(synthRows(3), moduleRows(1, 2)...)
	if got := CountModuleRows(rows); got != 2 {
		t.Errorf("CountModuleRows = %d, want 2", got)
	}
}

// TestIsModuleRow checks IsModuleRow distinguishes a module row from a
// package row.
func TestIsModuleRow(t *testing.T) {
	pkg := synthRows(1)[0]
	if IsModuleRow(&pkg) {
		t.Error("a package row reported as a module row")
	}
	mod := moduleRows(1)[0]
	if !IsModuleRow(&mod) {
		t.Error("a module row not reported as one")
	}
}

// TestModules checks Modules returns the distinct modules, sorted.
func TestModules(t *testing.T) {
	rows := []Row{{Module: "b"}, {Module: "a"}, {Module: "b"}, {Module: "c"}}
	got := Modules(rows)
	want := []string{"a", "b", "c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Modules = %v, want %v", got, want)
	}
}

// TestCheckLanguage checks CheckLanguage accepts rows of the named
// language, Go rows carrying no Language field, and rejects a mismatch.
func TestCheckLanguage(t *testing.T) {
	goRows := synthRows(2)
	tsRows := synthRows(2)
	for i := range tsRows {
		tsRows[i].Language = "typescript"
	}
	if err := CheckLanguage(goRows, ""); err != nil {
		t.Errorf("Go rows against \"\": %v", err)
	}
	if err := CheckLanguage(tsRows, "typescript"); err != nil {
		t.Errorf("typescript rows against typescript: %v", err)
	}
	if err := CheckLanguage(goRows, "typescript"); err == nil {
		t.Error("Go rows against typescript: want an error")
	}
	if err := CheckLanguage(tsRows, ""); err == nil {
		t.Error("typescript rows against \"\": want an error")
	}
}

// TestLoadRows checks LoadRows reads a packages.jsonl file and rejects a
// missing or empty one.
func TestLoadRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "packages.jsonl")
	if err := os.WriteFile(path, []byte(`{"module":"m","package":"p"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := LoadRows(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Module != "m" {
		t.Errorf("LoadRows = %+v, want one row for module m", rows)
	}
	if _, err := LoadRows(filepath.Join(dir, "missing.jsonl")); err == nil {
		t.Error("LoadRows of a missing file: want an error")
	}
	empty := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRows(empty); err == nil {
		t.Error("LoadRows of an empty file: want an error")
	}
}
