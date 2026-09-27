package engine

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
	"github.com/rfizzle/astimate/internal/report"
)

// measuringExtractor wraps an extractor with a canned CoverageMeasurer,
// recording the packages each Coverage call asked for.
type measuringExtractor struct {
	metrics.Extractor
	result map[string]metrics.Coverage
	err    error
	calls  [][]string
}

func (f *measuringExtractor) Coverage(_ context.Context, _ *metrics.ModuleContext, pkgs []string) (map[string]metrics.Coverage, error) {
	f.calls = append(f.calls, slices.Clone(pkgs))
	return f.result, f.err
}

// coverageTarget returns a Target over a fake module of three packages, two
// with test files, whose extractor is ext wrapped by wrap, logging to logs.
func coverageTarget(logs *bytes.Buffer, wrap func(metrics.Extractor) metrics.Extractor) *Target {
	const root, modPath = "/mod", "example.com/m"
	pkgs := map[string]metrics.RawMetrics{
		modPath + "/good":   {TokensEst: 1000, ExportedSymbols: 4, UntestedExports: 4, TestFiles: 1, TestFuncs: 1, HasTests: true},
		modPath + "/broken": {TokensEst: 1000, ExportedSymbols: 4, UntestedExports: 4, TestFiles: 1, TestFuncs: 1, HasTests: true},
		modPath + "/none":   {TokensEst: 1000, ExportedSymbols: 4, UntestedExports: 4},
	}
	return &Target{
		Mod:    &metrics.ModuleContext{Root: root, ModulePath: modPath},
		Ext:    wrap(metricstest.NewFake("go", root, pkgs)),
		Cfg:    &config.Config{Rebuild: rankParams()},
		Logger: slog.New(slog.NewTextHandler(logs, nil)),
	}
}

func TestRankCoverage(t *testing.T) {
	t.Parallel()

	pct := 50.0
	canned := map[string]metrics.Coverage{
		"example.com/m/good":   {Pct: &pct},
		"example.com/m/broken": {Reason: "test build failed: x_test.go:1:1: undefined: y"},
	}
	tests := []struct {
		name      string
		opts      CoverageOptions
		err       error
		wantCalls [][]string
		wantGood  float64 // rounded agent_passes of good, scaled when measured
		wantLogs  []string
		noLogs    []string
	}{
		{
			name:     "flag absent measures nothing",
			wantGood: 0.2, // (1000 + 4*40 + 4*800) / 25000 = 0.168
			noLogs:   []string{"coverage"},
		},
		{
			name:      "measures packages with test files in one run",
			opts:      CoverageOptions{Enabled: true},
			wantCalls: [][]string{{"example.com/m/broken", "example.com/m/good"}},
			wantGood:  0.1, // (1000 + 160 + 3200*0.5) / 25000 = 0.104
			wantLogs:  []string{"coverage not measured", "path=broken", "undefined: y"},
			noLogs:    []string{"path=none", "path=good"},
		},
		{
			name:      "a run that cannot start warns per package",
			opts:      CoverageOptions{Enabled: true},
			err:       errors.New("go: not found"),
			wantCalls: [][]string{{"example.com/m/broken", "example.com/m/good"}},
			wantGood:  0.2,
			wantLogs:  []string{"path=good", "path=broken", "go: not found"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			me := &measuringExtractor{result: canned, err: tt.err}
			tg := coverageTarget(&logs, func(e metrics.Extractor) metrics.Extractor {
				me.Extractor = e
				return me
			})
			rows, failed, err := Rank(t.Context(), tg, RankOptions{Sort: report.SortPasses, Coverage: tt.opts})
			if err != nil || len(failed) > 0 {
				t.Fatalf("Rank: %v %v", err, failed)
			}
			if !slices.EqualFunc(me.calls, tt.wantCalls, slices.Equal) {
				t.Errorf("Coverage calls = %v, want %v", me.calls, tt.wantCalls)
			}
			for _, r := range rows {
				if r.Path == "good" && (r.AgentPasses < tt.wantGood-1e-9 || r.AgentPasses > tt.wantGood+1e-9) {
					t.Errorf("good agent_passes = %v, want %v", r.AgentPasses, tt.wantGood)
				}
			}
			for _, w := range tt.wantLogs {
				if !strings.Contains(logs.String(), w) {
					t.Errorf("logs lack %q:\n%s", w, logs.String())
				}
			}
			for _, w := range tt.noLogs {
				if strings.Contains(logs.String(), w) {
					t.Errorf("logs contain %q:\n%s", w, logs.String())
				}
			}
		})
	}
}

func TestAssessCoverageUnsupported(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	tg := coverageTarget(&logs, func(e metrics.Extractor) metrics.Extractor { return e })
	tg.ImportPath, tg.Dir = "example.com/m/good", "good"
	r, err := Assess(t.Context(), tg, AssessOptions{Coverage: CoverageOptions{Enabled: true}})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if r.Metrics.CoveragePct != nil {
		t.Errorf("coverage_pct = %v, want null for an extractor that cannot measure it", *r.Metrics.CoveragePct)
	}
	if n := strings.Count(logs.String(), "coverage not measured"); n != 1 {
		t.Errorf("logged %d coverage warnings, want 1:\n%s", n, logs.String())
	}
}

func TestCheckCoverageHeadOnly(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	pct := 50.0
	me := &measuringExtractor{result: map[string]metrics.Coverage{"example.com/m/good": {Pct: &pct}}}
	tg := coverageTarget(&logs, func(e metrics.Extractor) metrics.Extractor {
		me.Extractor = e
		return me
	})
	// The baseline equals head but, like every baseline, has no coverage.
	base := map[string]metrics.RawMetrics{
		"example.com/m/good": {TokensEst: 1000, ExportedSymbols: 4, UntestedExports: 4, TestFiles: 1, TestFuncs: 1, HasTests: true},
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := baseline.Write(path, "", "example.com/m", TokenizerEst, base); err != nil {
		t.Fatal(err)
	}
	c, failed, err := Check(t.Context(), tg, CheckOptions{
		BaselineFile: path,
		Packages:     []string{"example.com/m/good"},
		Coverage:     CoverageOptions{Enabled: true},
	})
	if err != nil || len(failed) > 0 {
		t.Fatalf("Check: %v %v", err, failed)
	}
	if len(me.calls) != 1 {
		t.Fatalf("Coverage calls = %v, want one run", me.calls)
	}
	p := c.Packages[0].Report
	basePasses := c.Packages[0].BaseAgentPasses
	if p.Metrics.CoveragePct == nil || *p.Metrics.CoveragePct != 50 {
		t.Fatalf("head coverage_pct = %v, want 50", p.Metrics.CoveragePct)
	}
	if p.Baseline == nil || p.Baseline.Metrics.CoveragePct != nil {
		t.Errorf("baseline = %+v, want present with null coverage_pct", p.Baseline)
	}
	if basePasses == nil || *basePasses != p.Rebuild.AgentPasses {
		t.Errorf("base agent_passes = %v, head %v; want equal for an unchanged package measured at head only",
			basePasses, p.Rebuild.AgentPasses)
	}
}
