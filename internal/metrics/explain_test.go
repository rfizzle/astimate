package metrics

import (
	"strings"
	"testing"
)

func TestExplainCoversEveryMetric(t *testing.T) {
	names := MetricNames()
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			e, ok := Explain(name)
			if !ok {
				t.Fatalf("Explain(%q) has no entry", name)
			}
			if e.Definition == "" || e.Evidence == "" {
				t.Errorf("Explain(%q) = %+v, want a definition and evidence", name, e)
			}
			var zero RawMetrics
			_, v0 := zero.Value(name)
			want := "v1"
			if v0 {
				want = "v0"
			}
			if e.Release != want {
				t.Errorf("Explain(%q).Release = %q, want %q", name, e.Release, want)
			}
		})
	}
	if n := len(explanations()); n != len(names) {
		t.Errorf("explanations has %d entries, want %d, one per metric", n, len(names))
	}
	if _, ok := Explain("concrete_param_ratio"); ok {
		t.Error("Explain(concrete_param_ratio) has an entry for a removed metric")
	}
}

func TestExplainCouplingReportedNotGated(t *testing.T) {
	for _, name := range []string{"instability", "abstractness", "main_sequence_distance"} {
		t.Run(name, func(t *testing.T) {
			e, ok := Explain(name)
			if !ok {
				t.Fatalf("Explain(%q) has no entry", name)
			}
			if e.Gated {
				t.Errorf("Explain(%q).Gated = true, want false", name)
			}
			for _, want := range []string{"Reported, not gated", "consumer-defined interfaces", "leaf packages"} {
				if !strings.Contains(e.Evidence, want) {
					t.Errorf("Explain(%q).Evidence lacks %q: %s", name, want, e.Evidence)
				}
			}
		})
	}
}

// TestExplainCrossPackageAndOpacityNotGated checks that the four fields the
// Go extractor computes without a default threshold say so.
func TestExplainCrossPackageAndOpacityNotGated(t *testing.T) {
	tests := map[string]string{
		"dup_blocks_cross_pkg": "Measured before it is gated",
		"uses_cgo":             "not gated",
		"uses_reflect":         "not gated",
		"generated_files":      "not gated",
	}
	for name, want := range tests {
		e, ok := Explain(name)
		if !ok {
			t.Fatalf("Explain(%q) has no entry", name)
		}
		if e.Gated || !strings.Contains(e.Evidence, want) {
			t.Errorf("Explain(%q) = gated %v, evidence %q; want not gated, saying %q", name, e.Gated, e.Evidence, want)
		}
	}
	if e, _ := Explain("dup_blocks_cross_pkg"); !strings.Contains(e.Definition, `"module"`) {
		t.Errorf("dup_blocks_cross_pkg definition does not name the module row: %s", e.Definition)
	}
}
