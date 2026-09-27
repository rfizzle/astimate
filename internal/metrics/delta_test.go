package metrics

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestDeltaIdenticalIsZero(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    RawMetrics
	}{
		{name: "full", m: fullMetrics()},
		{name: "zero", m: RawMetrics{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.m.Delta(tc.m)
			for _, name := range MetricNames() {
				got, ok := d.Value(name)
				_, headOK := tc.m.Value(name)
				if ok != headOK {
					t.Errorf("%s: delta ok = %v, want %v", name, ok, headOK)
				}
				if got != 0 {
					t.Errorf("%s: delta = %v, want 0", name, got)
				}
			}
		})
	}
}

func TestDeltaNewPackageYieldsHeadValues(t *testing.T) {
	head := fullMetrics()
	d := head.Delta(RawMetrics{})
	for _, name := range MetricNames() {
		want, _ := head.Value(name)
		got, ok := d.Value(name)
		if !ok {
			t.Errorf("%s: delta missing", name)
		}
		if got != want {
			t.Errorf("%s: delta = %v, want head value %v", name, got, want)
		}
	}
}

func TestDeltaIncreasedAndDecreased(t *testing.T) {
	base := fullMetrics()
	head := fullMetrics()
	head.DupBlocks += 2
	head.UntestedExports -= 3
	head.DuplicationPct += 1.5
	head.HasTests = false
	head.UsesCgo = ptr(false)
	head.UsesReflect = ptr(true)
	head.CoveragePct = ptr(*base.CoveragePct - 10)
	head.GeneratedFiles = ptr(*base.GeneratedFiles + 4)

	d := head.Delta(base)
	want := map[string]float64{
		"dup_blocks":       2,
		"untested_exports": -3,
		"duplication_pct":  1.5,
		"has_tests":        -1,
		"uses_cgo":         -1,
		"uses_reflect":     1,
		"coverage_pct":     -10,
		"generated_files":  4,
	}
	for _, name := range MetricNames() {
		got, ok := d.Value(name)
		if !ok {
			t.Errorf("%s: delta missing", name)
			continue
		}
		if got != want[name] {
			t.Errorf("%s: delta = %v, want %v", name, got, want[name])
		}
	}

	// Reversing the comparison flips every sign.
	r := base.Delta(head)
	for name, w := range want {
		if got, _ := r.Value(name); got != -w {
			t.Errorf("reverse %s: delta = %v, want %v", name, got, -w)
		}
	}
}

func TestDeltaV1NotComputedAtHeadIsNull(t *testing.T) {
	head := RawMetrics{}
	d := head.Delta(fullMetrics())
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, name := range []string{
		"concrete_param_ratio", "dup_blocks_cross_pkg", "uses_cgo", "uses_reflect",
		"generated_files", "coverage_pct", "changed_func_cognitive_max",
	} {
		if got := string(fields[name]); got != "null" {
			t.Errorf("%s = %s, want null", name, got)
		}
		if _, ok := d.Value(name); ok {
			t.Errorf("%s: Value ok for nil delta", name)
		}
	}
}

func TestMetricDeltasJSONNames(t *testing.T) {
	head := fullMetrics()
	d := head.Delta(RawMetrics{})
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := jsonKeys(t, data), MetricNames(); !slices.Equal(got, want) {
		t.Errorf("JSON keys = %v, want %v", got, want)
	}
	if _, ok := d.Value("no_such_metric"); ok {
		t.Error("Value of unknown name reported ok")
	}
}
