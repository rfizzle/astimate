package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/metrics"
)

// dataPath is the committed standard-library data the candidate is fitted
// from.
const dataPath = "../data/2026-09-27/packages.jsonl"

// committedCandidate is the candidate fitted from dataPath.
const committedCandidate = "../thresholds/astimate-thresholds-2026-09-27-stdlib-provisional.yaml"

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
	base, err := config.Parse([]byte(strings.Replace(string(config.Default()), "thresholds:\n", "thresholds:\n"+
		"  - metric: globals\n    kind: density\n    max_delta: 0\n", 1)))
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
		{"globals", "none", "1"},            // density without a max stays without; all zero, minimum 1
		{"has_tests", "none", "none"},       // requirement untouched
		{"tokens_est", "1", "none"},         // all zero: a capacity max is at least one step
		// Null in every row (it needs a baseline diff): the base max stays,
		// and a max-only density rule gets no max_delta.
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

// fitInto fits the committed data into dir and returns the candidate's
// path and the report.
func fitInto(t *testing.T, dir string) (candidate, report string) {
	t.Helper()
	opts := options{
		data:   dataPath,
		out:    filepath.Join(dir, "candidate.yaml"),
		report: filepath.Join(dir, "report.md"),
		date:   "2026-09-27",
		suffix: suffixAuto,
	}
	res, err := fit(opts)
	if err != nil {
		t.Fatalf("fit: %v", err)
	}
	if res.version != "thresholds-2026-09-27-stdlib-provisional" {
		t.Errorf("version = %q, want the standard-library provisional suffix", res.version)
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
	base, err := config.Parse(config.Default())
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
	if !strings.Contains(report, "**Provisional.**") {
		t.Error("report does not say the candidate is provisional")
	}
}

// TestCommittedCandidate checks the committed candidate validates.
func TestCommittedCandidate(t *testing.T) {
	cfg, err := config.Load(committedCandidate)
	if err != nil {
		t.Fatalf("committed candidate does not validate: %v", err)
	}
	if cfg.Version != "thresholds-2026-09-27-stdlib-provisional" {
		t.Errorf("config_version = %q", cfg.Version)
	}
}

// TestCandidatePassesGoodCode is the acceptance check: under the fitted
// candidate the fixture passes check --all against its own baseline, and
// the standard-library errors package passes both against itself and as a
// package new at head.
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

	t.Run("errors", func(t *testing.T) {
		m, err := golang.ExtractStdlib(ctx, "errors",
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
