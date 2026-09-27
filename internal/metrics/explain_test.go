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
