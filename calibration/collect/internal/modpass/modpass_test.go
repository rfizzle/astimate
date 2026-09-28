package modpass

import (
	"context"
	"errors"
	"testing"

	astmetrics "github.com/rfizzle/astimate/internal/metrics"
)

// stubModule implements astmetrics.ModuleMetrics for the test.
type stubModule struct {
	m   astmetrics.RawMetrics
	err error
}

func (s stubModule) ModuleRow(context.Context, *astmetrics.ModuleContext) (astmetrics.RawMetrics, error) {
	return s.m, s.err
}

// TestMeasure checks Measure tags the module row with its module path,
// commit, the module row package id, the measured metrics and packages
// count, and records a pass cost.
func TestMeasure(t *testing.T) {
	cross := 3
	stub := stubModule{m: astmetrics.RawMetrics{DupBlocksCrossPkg: &cross}}
	row, err := Measure(t.Context(), stub, &astmetrics.ModuleContext{}, "example.com/m", "deadbeef", 12, 42)
	if err != nil {
		t.Fatal(err)
	}
	if row.Module != "example.com/m" || row.Commit != "deadbeef" || row.Package != astmetrics.ModuleRowID {
		t.Errorf("row identifiers = %+v, want example.com/m, deadbeef, %s", row, astmetrics.ModuleRowID)
	}
	if row.Packages != 12 {
		t.Errorf("row.Packages = %d, want 12", row.Packages)
	}
	if row.Metrics.DupBlocksCrossPkg == nil || *row.Metrics.DupBlocksCrossPkg != 3 {
		t.Errorf("row.Metrics.DupBlocksCrossPkg = %v, want 3", row.Metrics.DupBlocksCrossPkg)
	}
	if row.Cost.LoadMS != 42 {
		t.Errorf("row.Cost.LoadMS = %d, want 42", row.Cost.LoadMS)
	}
	if row.Cost.PassMS < 0 {
		t.Errorf("row.Cost = %+v, want a non-negative pass time", row.Cost)
	}
}

// TestMeasureError checks Measure wraps the underlying ModuleRow error with
// the module path.
func TestMeasureError(t *testing.T) {
	boom := errors.New("boom")
	_, err := Measure(t.Context(), stubModule{err: boom}, &astmetrics.ModuleContext{}, "example.com/m", "c", 1, 1)
	if err == nil || !errors.Is(err, boom) {
		t.Errorf("err = %v, want it to wrap boom", err)
	}
}
