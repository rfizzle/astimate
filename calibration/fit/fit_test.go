package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/metrics"
)

// dataPath is the committed corpus data the shipped thresholds are fitted
// from: the standard library and the cloned modules of corpus.yaml, with
// per-function cognitive counts.
const dataPath = "../data/2026-09-28-corpus/packages.jsonl"

// modulesPath is the committed module rows of the cloned corpus modules,
// which dup_blocks_cross_pkg is fitted from.
const modulesPath = "../data/2026-09-28-modules/modules.jsonl"

// previousDataPath is the earlier corpus data, collected before the rows
// carried per-function counts.
const previousDataPath = "../data/2026-09-27-corpus/packages.jsonl"

// previousCandidate is the candidate fitted from previousDataPath.
const previousCandidate = "../thresholds/astimate-thresholds-2026-09-27.yaml"

// uncalibratedBase is the base the committed candidates are fitted on.
const uncalibratedBase = "../../configs/uncalibrated.yaml"

// stdlibDataPath is the earlier standard-library-only data, kept for
// comparison; a fit of it carries the provisional suffix.
const stdlibDataPath = "../data/2026-09-27/packages.jsonl"

// committedCandidate is the candidate fitted from dataPath, which the
// embedded default carries.
const committedCandidate = "../thresholds/astimate-thresholds-2026-09-28.yaml"

// shippedVersion is committedCandidate's config_version.
const shippedVersion = "thresholds-2026-09-28"

func TestPercentile(t *testing.T) {
	series := make([]float64, 100)
	for i := range series {
		series[i] = float64(i + 1)
	}
	tests := []struct {
		name   string
		values []float64
		p      float64
		want   float64
	}{
		{"1..100 p25", series, 25, 25},
		{"1..100 p50", series, 50, 50},
		{"1..100 p75", series, 75, 75},
		{"1..100 p90", series, 90, 90},
		{"1..100 p95", series, 95, 95},
		{"1..100 p0", series, 0, 1},
		{"1..100 p100", series, 100, 100},
		{"four values p50", []float64{1, 2, 3, 4}, 50, 2},
		{"four values p90", []float64{1, 2, 3, 4}, 90, 4},
		{"single", []float64{7}, 90, 7},
		{"empty", nil, 90, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := percentile(tt.values, tt.p); got != tt.want {
				t.Errorf("percentile(p%v) = %v, want %v", tt.p, got, tt.want)
			}
		})
	}
}

func TestComputeStats(t *testing.T) {
	values := make([]float64, 100)
	for i := range values {
		// Reversed, so computeStats must sort.
		values[i] = float64(100 - i)
	}
	s := computeStats(values)
	if s.N != 100 || s.Min != 1 || s.Max != 100 || s.P50 != 50 || s.P90 != 90 || s.IQR != 50 {
		t.Errorf("stats = %+v, want n 100, min 1, max 100, p50 50, p90 90, IQR 50", s)
	}
	if len(s.Hist) != histBins {
		t.Fatalf("histogram has %d bins, want %d", len(s.Hist), histBins)
	}
	total := 0
	for _, b := range s.Hist {
		total += b.Count
	}
	if total != 100 {
		t.Errorf("histogram counts %d values, want 100", total)
	}
	if last := s.Hist[histBins-1]; !last.Open || last.Count != 5 || last.Lo != 95 {
		t.Errorf("open bin = %+v, want the 5 values above p95 95", last)
	}
	if first := s.Hist[0]; first.Count != 11 {
		// Bins over [1, 95] are 94/9 wide: 1..11 fall in the first.
		t.Errorf("first bin = %+v, want 11 values", first)
	}
}

func TestHistogramConstant(t *testing.T) {
	s := computeStats([]float64{3, 3, 3})
	if s.Hist[0].Count != 3 || s.Hist[histBins-1].Count != 0 {
		t.Errorf("constant series histogram = %+v, want every value in the first bin", s.Hist)
	}
}

func TestRoundReadable(t *testing.T) {
	tests := []struct {
		v       float64
		percent bool
		want    float64
	}{
		{0, false, 0},
		{0.26, true, 0.5},
		{0.3, false, 0},
		{6, false, 6},
		{7.3, false, 7},
		{7.3, true, 7.5},
		{9.74, true, 9.5},
		{10, false, 10},
		{12.5, false, 15}, // 13 at two significant figures
		{16, false, 15},
		{23, false, 25},
		{48.7, true, 50},
		{100, false, 100},
		{101, false, 100},
		{874, false, 850}, // 870 at two significant figures
		{899, false, 900},
		{1000, false, 1000},
		{1234, false, 1000},
		{1250, false, 1500}, // 1300 at two significant figures
		{2325, false, 2500},
		{30864, false, 31000},
		{789448, false, 790000},
	}
	for _, tt := range tests {
		if got := roundReadable(tt.v, tt.percent); got != tt.want {
			t.Errorf("roundReadable(%v, percent %t) = %v, want %v", tt.v, tt.percent, got, tt.want)
		}
	}
}

func TestDeltaFromIQR(t *testing.T) {
	tests := []struct {
		iqr     float64
		percent bool
		want    float64
	}{
		{0, false, 1},
		{0, true, 0.5},
		{2, false, 1},
		{2, true, 0.5},
		{3, false, 1},
		{8, false, 2},
		{9, false, 3},
		{11, false, 3},
		{29.5, true, 7.5},
		{30, true, 7.5},
		{30.1, true, 8},
	}
	for _, tt := range tests {
		if got := deltaFromIQR(tt.iqr, tt.percent); got != tt.want {
			t.Errorf("deltaFromIQR(%v, percent %t) = %v, want %v", tt.iqr, tt.percent, got, tt.want)
		}
	}
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

func TestFitThresholds(t *testing.T) {
	base, err := config.Load(uncalibratedBase)
	if err != nil {
		t.Fatal(err)
	}
	choices := fitThresholds(synthRows(100), base)
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
		if got := opt(c.Max); got != tt.max {
			t.Errorf("%s: max = %s, want %s", tt.metric, got, tt.max)
		}
		if got := opt(c.MaxDelta); got != tt.maxDelta {
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
	base, err := config.Load(uncalibratedBase)
	if err != nil {
		t.Fatal(err)
	}
	// 200 rows of one function each, cognitive 1..200, half of them std;
	// p99 is 198, which rounds to 200. A 201st row has no functions.
	rows := synthRows(201)
	for i := range 200 {
		rows[i].FuncCognitive = map[int]int{i + 1: 1}
		if i%2 == 0 {
			rows[i].Module = stdModule
		}
	}
	var c *Choice
	for _, ch := range fitThresholds(rows, base) {
		if ch.Rule.Metric == funcMetric {
			c = &ch
		}
	}
	if c == nil {
		t.Fatal("changed_func_cognitive_max not fitted")
	}
	if c.Pool != poolFunctions || c.Stats.N != 200 || c.Stats.P90 != 180 || c.Stats.P99 != 198 {
		t.Errorf("pool %q n %d p90 %v p99 %v, want 200 functions of all rows, p90 180, p99 198",
			c.Pool, c.Stats.N, c.Stats.P90, c.Stats.P99)
	}
	if opt(c.Max) != "200" || opt(c.MaxDelta) != "none" {
		t.Errorf("max %s max_delta %s, want p99 rounded to 200 and no max_delta", opt(c.Max), opt(c.MaxDelta))
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
	for _, c := range fitThresholds(rows, base) {
		if c.Rule.MaxDelta == nil {
			continue
		}
		switch {
		case *c.Rule.MaxDelta == 0:
			pinned++
			if c.MaxDelta == nil || *c.MaxDelta != 0 || !c.DeltaPinned {
				t.Errorf("%s: max_delta = %s (pinned %t) from IQR %v, want 0 kept", c.Rule.Metric, opt(c.MaxDelta), c.DeltaPinned, c.Stats.IQR)
			}
		case c.DeltaPinned:
			t.Errorf("%s: base max_delta %v marked pinned", c.Rule.Metric, *c.Rule.MaxDelta)
		}
	}
	if pinned != 6 {
		t.Errorf("%d rules pinned, want the 6 zero-tolerance rules of the default", pinned)
	}
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
	for _, ch := range fitThresholds(rows, base) {
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
	if c.Pool != poolModule || c.Stats.N != 10 || c.Stats.P90 != 90 {
		t.Errorf("pool %q n %d p90 %v, want 10 module rows and p90 90", c.Pool, c.Stats.N, c.Stats.P90)
	}
	if opt(c.Max) != "90" || opt(c.MaxDelta) != "0" || !c.DeltaPinned {
		t.Errorf("max %s max_delta %s pinned %t, want 90, 0 and pinned", opt(c.Max), opt(c.MaxDelta), c.DeltaPinned)
	}
	if c.OverCandidate != 1 {
		t.Errorf("%d modules fail the candidate as new, want the one above 90", c.OverCandidate)
	}
}

// TestFitModulesFile fits the committed data with the committed module
// rows and checks the report's cross-package section and the fitted
// dup_blocks_cross_pkg rule, and that a modules file holding a package row
// is rejected.
func TestFitModulesFile(t *testing.T) {
	dir := t.TempDir()
	res, err := fit(options{data: dataPath, modules: modulesPath, base: uncalibratedBase,
		out: filepath.Join(dir, "c.yaml"), report: filepath.Join(dir, "r.md"), date: "2026-09-28", suffix: suffixAuto})
	if err != nil {
		t.Fatalf("fit: %v", err)
	}
	data, err := os.ReadFile(res.report)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"- Module rows: `" + modulesPath + "`, 36 `<module>` rows", "## Cross-package duplication",
		"| Modules | p25 | p50 | p75 | p90 | max |", "| 36 | 1 | 9 | 104 | 443 | 3394 |", "### `dup_blocks_cross_pkg` (density)",
		"Rows: 36, module rows."} {
		if !strings.Contains(string(data), want) {
			t.Errorf("report does not contain %q", want)
		}
	}
	cfg, err := config.Load(res.out)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range cfg.Thresholds {
		if r.Metric == "dup_blocks_cross_pkg" && (opt(r.Max) != "450" || opt(r.MaxDelta) != "0" || r.RatchetFromZero) {
			t.Errorf("dup_blocks_cross_pkg max %s max_delta %s ratchet %t, want 450, 0, false", opt(r.Max), opt(r.MaxDelta), r.RatchetFromZero)
		}
	}
	_, err = fit(options{data: dataPath, modules: dataPath, base: uncalibratedBase,
		out: filepath.Join(dir, "c2.yaml"), report: filepath.Join(dir, "r2.md"), date: "2026-09-28", suffix: suffixAuto})
	if err == nil || !strings.Contains(err.Error(), "not a module row") {
		t.Errorf("fit with package rows as modules: err = %v, want a module-row error", err)
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
	for _, c := range fitThresholds(rows, base) {
		switch c.Rule.Metric {
		case "internal_imports":
			if c.Pool != poolCloned || c.Stats.N != 10 || c.Stats.Max != 3 || opt(c.Max) != "3" {
				t.Errorf("internal_imports: pool %q n %d max %v candidate %s, want the 10 cloned rows and max 3",
					c.Pool, c.Stats.N, c.Stats.Max, opt(c.Max))
			}
		case "sloc":
			if c.Pool != poolAll || c.Stats.N != 20 {
				t.Errorf("sloc: pool %q n %d, want all 20 rows", c.Pool, c.Stats.N)
			}
		}
	}
}

// fitInto fits the committed data into dir and returns the candidate's
// path and the report.
func fitInto(t *testing.T, dir string) (candidate, report string) {
	t.Helper()
	opts := options{
		data:    dataPath,
		modules: modulesPath,
		out:     filepath.Join(dir, "candidate.yaml"),
		report:  filepath.Join(dir, "report.md"),
		date:    "2026-09-28",
		suffix:  suffixAuto,
		base:    uncalibratedBase,
	}
	res, err := fit(opts)
	if err != nil {
		t.Fatalf("fit: %v", err)
	}
	if res.version != shippedVersion {
		t.Errorf("version = %q, want %q with no suffix", res.version, shippedVersion)
	}
	data, err := os.ReadFile(res.report)
	if err != nil {
		t.Fatal(err)
	}
	return res.out, string(data)
}

// TestCandidate fits the committed data and checks the candidate validates,
// keeps every non-threshold key of the base, and is described for every
// gated metric by the report.
func TestCandidate(t *testing.T) {
	path, report := fitInto(t, t.TempDir())
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("candidate does not validate: %v", err)
	}
	base, err := config.Load(uncalibratedBase)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Rebuild != base.Rebuild || cfg.CharsPerToken != base.CharsPerToken || cfg.Duplication != base.Duplication {
		t.Errorf("candidate changed the rebuild or extraction settings")
	}
	if len(cfg.Thresholds) != len(base.Thresholds) {
		t.Fatalf("candidate has %d rules, base %d", len(cfg.Thresholds), len(base.Thresholds))
	}
	for i, r := range cfg.Thresholds {
		b := base.Thresholds[i]
		if r.Metric != b.Metric || r.Kind != b.Kind || r.WarnAt != b.WarnAt || r.RatchetFromZero != b.RatchetFromZero ||
			(r.Max == nil) != (b.Max == nil) || (r.MaxDelta == nil) != (b.MaxDelta == nil) {
			t.Errorf("thresholds[%d] %s: shape changed from the base", i, r.Metric)
		}
		if !strings.Contains(report, "### `"+r.Metric+"` ("+string(r.Kind)+")") {
			t.Errorf("report has no section for %s", r.Metric)
		}
	}
	for _, want := range []string{"cloned-module rows only", "max_delta stays 0 on", "## Cross-package duplication",
		"## Per-function cognitive complexity", "| Functions | p50 | p90 | p99 | max |"} {
		if !strings.Contains(report, want) {
			t.Errorf("report does not contain %q", want)
		}
	}
	if strings.Contains(report, "**Provisional.**") {
		t.Error("report calls the corpus fit provisional")
	}
}

// TestPreviousDataKeepsFunctionBase fits the earlier corpus data, whose
// rows carry no per-function counts, and checks changed_func_cognitive_max
// keeps the base max and the report says why, while the comparison with
// the earlier candidate finds nothing moved: the fit reproduces it.
func TestPreviousDataKeepsFunctionBase(t *testing.T) {
	dir := t.TempDir()
	res, err := fit(options{data: previousDataPath, base: uncalibratedBase, compare: previousCandidate,
		out: filepath.Join(dir, "c.yaml"), report: filepath.Join(dir, "r.md"), date: "2026-09-27", suffix: suffixAuto})
	if err != nil {
		t.Fatalf("fit: %v", err)
	}
	data, err := os.ReadFile(res.report)
	if err != nil {
		t.Fatal(err)
	}
	// The earlier data has no module rows either, and dup_blocks_cross_pkg
	// keeps the base's placeholder, which the earlier candidate lacked.
	for _, want := range []string{"`changed_func_cognitive_max`: no row counts its functions", "## Against thresholds-2026-09-27",
		"`dup_blocks_cross_pkg`: the data has no `<module>` rows", "Only `dup_blocks_cross_pkg` moved"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("report does not contain %q", want)
		}
	}
}

// TestCompareNamesMovedRules fits the committed data and module rows
// against the earlier candidate and checks the report names
// changed_func_cognitive_max and the new dup_blocks_cross_pkg rule as the
// only rules that moved.
func TestCompareNamesMovedRules(t *testing.T) {
	dir := t.TempDir()
	res, err := fit(options{data: dataPath, modules: modulesPath, base: uncalibratedBase, compare: previousCandidate,
		out: filepath.Join(dir, "c.yaml"), report: filepath.Join(dir, "r.md"), date: "2026-09-28", suffix: suffixAuto})
	if err != nil {
		t.Fatalf("fit: %v", err)
	}
	data, err := os.ReadFile(res.report)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Only `changed_func_cognitive_max`, `dup_blocks_cross_pkg` moved; every other limit is unchanged."; !strings.Contains(string(data), want) {
		t.Errorf("report does not contain %q", want)
	}
}

// TestStdlibOnlyIsProvisional checks a fit of standard-library rows alone
// carries the provisional suffix.
func TestStdlibOnlyIsProvisional(t *testing.T) {
	dir := t.TempDir()
	res, err := fit(options{data: stdlibDataPath, out: filepath.Join(dir, "c.yaml"), report: filepath.Join(dir, "r.md"),
		date: "2026-09-27", suffix: suffixAuto})
	if err != nil {
		t.Fatalf("fit: %v", err)
	}
	if res.version != "thresholds-2026-09-27-stdlib-provisional" {
		t.Errorf("version = %q, want the standard-library provisional suffix", res.version)
	}
}

// TestCommittedCandidate checks the committed candidate validates.
func TestCommittedCandidate(t *testing.T) {
	cfg, err := config.Load(committedCandidate)
	if err != nil {
		t.Fatalf("committed candidate does not validate: %v", err)
	}
	if cfg.Version != shippedVersion {
		t.Errorf("config_version = %q, want %q", cfg.Version, shippedVersion)
	}
}

// TestDefaultIsCommittedCandidate checks the embedded default carries the
// committed candidate's config_version and exactly its rules, so the
// shipped thresholds are the fitted ones.
func TestDefaultIsCommittedCandidate(t *testing.T) {
	cand, err := config.Load(committedCandidate)
	if err != nil {
		t.Fatal(err)
	}
	def, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	if def.Version != cand.Version {
		t.Errorf("default config_version = %q, candidate %q", def.Version, cand.Version)
	}
	if !reflect.DeepEqual(def.Thresholds, cand.Thresholds) {
		t.Errorf("default thresholds differ from the committed candidate:\n%+v\n%+v", def.Thresholds, cand.Thresholds)
	}
	if def.Rebuild != cand.Rebuild || def.CharsPerToken != cand.CharsPerToken || def.Duplication != cand.Duplication {
		t.Error("default rebuild or extraction settings differ from the committed candidate")
	}
}

// TestCandidatePassesGoodCode is the acceptance check: under the fitted
// candidate the fixture passes check --all against its own baseline, and
// the standard-library sort package passes against itself and, as a
// package new at head, every rule but the zero-tolerance ratchets, which
// judge what a change adds rather than where a package sits in the corpus.
// (errors, used before, sits above the corpus p90 of cognitive_p90 and
// fails the fitted max as a new package, as a tenth of the corpus does by
// construction.)
func TestCandidatePassesGoodCode(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the fixture and the standard library")
	}
	path, _ := fitInto(t, t.TempDir())
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	t.Run("fixture self-baseline", func(t *testing.T) {
		tg, err := engine.LoadTarget("../../testdata/go/fixture", engine.TargetOptions{Config: cfg})
		if err != nil {
			t.Fatal(err)
		}
		pkgs, err := baseline.Collect(ctx, tg.Ext, tg.Mod)
		if err != nil {
			t.Fatal(err)
		}
		self := filepath.Join(t.TempDir(), "baseline.json")
		if err := baseline.Write(self, "self", tg.Mod.ModulePath, baseline.DefaultTokenizer, pkgs); err != nil {
			t.Fatal(err)
		}
		c, failed, err := engine.Check(ctx, tg, engine.CheckOptions{BaselineFile: self, All: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(failed) > 0 {
			t.Fatalf("packages failed to extract: %v", failed)
		}
		if len(c.Packages) == 0 {
			t.Fatal("check selected no packages")
		}
		for _, p := range c.Packages {
			for _, v := range p.Report.Violations {
				t.Errorf("%s: violation %+v", p.Report.PackagePath, v)
			}
		}
	})

	t.Run("sort", func(t *testing.T) {
		m, err := golang.ExtractStdlib(ctx, "sort",
			golang.WithCharsPerToken(cfg.CharsPerToken),
			golang.WithDupMinTokens(cfg.Duplication.MinTokens),
			golang.WithDupIgnoreLiteralOnly(cfg.Duplication.IgnoreLiteralOnly),
			golang.WithDupFoldSigns(cfg.Duplication.FoldSigns))
		if err != nil {
			t.Skipf("toolchain cannot load the standard library: %v", err)
		}
		for _, tt := range []struct {
			name string
			base *metrics.RawMetrics
		}{{"self-baseline", &m}, {"new package", nil}} {
			res := gate.Evaluate(m, tt.base, cfg.Thresholds, nil)
			for _, v := range res.Violations {
				if tt.base == nil && v.Limit == "max_delta +0" {
					continue
				}
				t.Errorf("%s: violation %+v", tt.name, v)
			}
		}
	})
}

func TestRunUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"--data", "x", "--date", "27-09-2026"}, {"--data", "x", "extra"}} {
		if got := run(args, io.Discard, io.Discard); got != 2 {
			t.Errorf("run(%q) = %d, want 2", args, got)
		}
	}
}
