package judge

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
)

// fakeBaseline is a baseline.Baseline over a map of package metrics that
// records no functions, blocks or import graph.
type fakeBaseline map[string]metrics.RawMetrics

func (b fakeBaseline) Metrics(pkg string) (metrics.RawMetrics, bool) {
	m, ok := b[pkg]
	return m, ok
}
func (fakeBaseline) Ref() string                                     { return "base" }
func (fakeBaseline) Tokenizer() string                               { return "est" }
func (fakeBaseline) Functions(string) ([]metrics.FunctionInfo, bool) { return nil, false }
func (fakeBaseline) CrossBlocks() ([]metrics.CrossBlock, bool)       { return nil, false }
func (fakeBaseline) Importers(string) ([]string, bool)               { return nil, false }

// checker returns a Checker over a fake module of one package a, whose
// globals rose from base, gated by a zero-tolerance globals rule and a
// dup_blocks_cross_pkg rule on the module row.
func checker(t *testing.T, row *int, exemptions []gate.Exemption) *Checker {
	t.Helper()
	const root, modPath = "/mod", "example.com/m"
	pkgs := map[string]metrics.RawMetrics{modPath + "/a": {Globals: 2}}
	details := map[string]metrics.Details{modPath + "/a": {
		GlobalPositions: []metrics.Position{{File: "a.go", Line: 3}, {File: "a.go", Line: 4}},
		GlobalNames:     []string{"x", "y"},
	}}
	opts := []metricstest.FakeOption{metricstest.WithDetails(details)}
	if row != nil {
		opts = append(opts, metricstest.WithModuleRow(metrics.RawMetrics{DupBlocksCrossPkg: row}))
	}
	zero := 0.0
	return New(Options{
		Ext:  metricstest.NewFake("go", root, pkgs, opts...),
		Mod:  &metrics.ModuleContext{Root: root, ModulePath: modPath},
		Base: fakeBaseline{modPath + "/a": {Globals: 1}, metrics.ModuleRowID: {DupBlocksCrossPkg: new(int)}},
		Config: config.Effective{Version: "v", Exemptions: exemptions, Thresholds: []gate.Threshold{
			{Metric: "globals", Kind: gate.Density, MaxDelta: &zero},
			{Metric: "dup_blocks_cross_pkg", Kind: gate.Density, MaxDelta: &zero},
		}},
		Tokenizer: "est",
		Now:       time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC),
		Logger:    slog.New(slog.DiscardHandler),
	})
}

func TestCheckerPackage(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		exemptions []gate.Exemption
		wantPassed bool
	}{
		{name: "new global fails", wantPassed: false},
		{name: "exempted", exemptions: []gate.Exemption{{Package: "a", Metric: "globals", Reason: "r"}}, wantPassed: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			jc := checker(t, nil, tt.exemptions)
			p, unrecorded, err := jc.Package(t.Context(), "example.com/m/a", metrics.RawMetrics{Globals: 2})
			if err != nil || unrecorded {
				t.Fatalf("Package = %v, unrecorded %v", err, unrecorded)
			}
			r := p.Report
			if r.PackagePath != "a" || r.Passed == nil || *r.Passed != tt.wantPassed || p.BaseAgentPasses == nil {
				t.Fatalf("report %s passed %v base passes %v, want a passed %v with base passes",
					r.PackagePath, r.Passed, p.BaseAgentPasses, tt.wantPassed)
			}
			for _, f := range r.AllFindings() {
				if f.Metric == "globals" && (f.Location == nil || f.Location.File != "a/a.go" || f.Location.Line != 3) {
					t.Errorf("globals located at %+v, want a/a.go:3", f.Location)
				}
			}
		})
	}
}

func TestCheckerModule(t *testing.T) {
	t.Parallel()

	one := 1
	jc := checker(t, &one, nil)
	p, judged, err := jc.Module(t.Context(), jc.o.Ext.(metrics.ModuleMetrics), false, nil)
	if err != nil || !judged {
		t.Fatalf("Module = %v, judged %v", err, judged)
	}
	if p.Report.PackagePath != metrics.ModuleRowID || p.Report.Passed == nil || *p.Report.Passed {
		t.Errorf("module row %s passed %v, want %s failing on a new cross-package block",
			p.Report.PackagePath, p.Report.Passed, metrics.ModuleRowID)
	}
	if _, _, err := jc.Module(t.Context(), failingRow{}, false, nil); err == nil {
		t.Error("Module with a failing row returned no error")
	}
}

// failingRow is a metrics.ModuleMetrics whose row fails.
type failingRow struct{}

func (failingRow) ModuleRow(context.Context, *metrics.ModuleContext) (metrics.RawMetrics, error) {
	return metrics.RawMetrics{}, errors.New("no row")
}

func TestDetails(t *testing.T) {
	t.Parallel()

	jc := checker(t, nil, nil)
	det, names, err := Details(t.Context(), jc.o.Ext, jc.o.Mod, "example.com/m/a")
	if err != nil || len(det.GlobalNames) != 2 || len(names.Globals) != 2 {
		t.Fatalf("Details = %+v, %+v, %v; want the two globals named", det, names, err)
	}
	if _, _, err := Details(t.Context(), jc.o.Ext, jc.o.Mod, "example.com/m/absent"); err == nil {
		t.Error("Details of an unknown package returned no error")
	}
}
