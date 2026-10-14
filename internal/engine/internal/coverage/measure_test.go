package coverage

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
)

// cannedMeasurer wraps an extractor with a canned CoverageMeasurer,
// recording the packages each Coverage call asked for.
type cannedMeasurer struct {
	metrics.Extractor
	result map[string]metrics.Coverage
	calls  [][]string
}

func (f *cannedMeasurer) Coverage(_ context.Context, _ *metrics.ModuleContext, pkgs []string) (map[string]metrics.Coverage, error) {
	f.calls = append(f.calls, slices.Clone(pkgs))
	return f.result, nil
}

func TestMeasure(t *testing.T) {
	t.Parallel()

	const root, modPath = "/mod", "example.com/m"
	pct, covered := 50.0, 1
	tests := []struct {
		name      string
		opts      Options
		measurer  bool
		wantCalls [][]string
		wantPct   bool // good's coverage_pct is set
		wantLogs  []string
	}{
		{name: "disabled measures nothing", measurer: true},
		{
			name: "measures candidates in order and logs the failures", opts: Options{Enabled: true}, measurer: true,
			wantCalls: [][]string{{modPath + "/good", modPath + "/broken"}}, wantPct: true,
			wantLogs: []string{"coverage not measured", "path=broken", `reason="no test binary"`},
		},
		{
			name: "an extractor that cannot run tests logs once", opts: Options{Enabled: true},
			wantLogs: []string{"the extractor cannot run tests"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ms := map[string]*metrics.RawMetrics{
				modPath + "/good":   {TestFiles: 1},
				modPath + "/broken": {UntestedExports: 2},
				modPath + "/bare":   {},
			}
			order := []string{modPath + "/good", modPath + "/broken", modPath + "/bare"}
			ext := metricstest.NewFake("go", root, nil)
			cm := &cannedMeasurer{Extractor: ext, result: map[string]metrics.Coverage{
				modPath + "/good":   {Pct: &pct, UntestedExports: &covered},
				modPath + "/broken": {Reason: "no test binary"},
			}}
			if tt.measurer {
				ext = cm
			}
			var logs bytes.Buffer
			Measure(t.Context(), Run{
				Ext: ext, Mod: &metrics.ModuleContext{Root: root, ModulePath: modPath},
				Options: tt.opts, Logger: slog.New(slog.NewTextHandler(&logs, nil)),
			}, ms, order)
			if !slices.EqualFunc(cm.calls, tt.wantCalls, slices.Equal) {
				t.Errorf("Coverage calls = %v, want %v", cm.calls, tt.wantCalls)
			}
			good := ms[modPath+"/good"]
			if got := good.CoveragePct != nil && good.UntestedExportsCovered != nil; got != tt.wantPct {
				t.Errorf("good measured = %v, want %v", got, tt.wantPct)
			}
			if ms[modPath+"/bare"].CoveragePct != nil {
				t.Error("bare, with no tests and no untested exports, was measured")
			}
			for _, want := range tt.wantLogs {
				if !strings.Contains(logs.String(), want) {
					t.Errorf("logs lack %q:\n%s", want, logs.String())
				}
			}
			if len(tt.wantLogs) == 0 && logs.Len() > 0 {
				t.Errorf("unexpected logs:\n%s", logs.String())
			}
		})
	}
}
