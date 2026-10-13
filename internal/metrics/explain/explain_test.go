package explain

import (
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

func TestExplainCoversEveryMetric(t *testing.T) {
	names := metrics.MetricNames()
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			e, ok := Explain(name)
			if !ok {
				t.Fatalf("Explain(%q) has no entry", name)
			}
			if e.Definition == "" || e.Evidence == "" {
				t.Errorf("Explain(%q) = %+v, want a definition and evidence", name, e)
			}
			var zero metrics.RawMetrics
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
	for _, name := range names {
		if e, _ := Explain(name); strings.ContainsAny(e.Definition+e.Evidence, "{}") {
			t.Errorf("Explain(%q) keeps an unexpanded placeholder: %+v", name, e)
		}
	}
	if _, ok := Explain("concrete_param_ratio"); ok {
		t.Error("Explain(concrete_param_ratio) has an entry for a removed metric")
	}
}

// TestTextsMatchFieldTable checks that texts has exactly one entry per
// RawMetrics field, in the field table's order, and only the known flags:
// a field added to metrics without an explanation, or an explanation left
// behind by a removed field, fails here.
func TestTextsMatchFieldTable(t *testing.T) {
	var names []string
	for _, e := range texts() {
		names = append(names, e.name)
		for w := range strings.FieldsSeq(e.flags) {
			if w != "gated" && w != "capacity" {
				t.Errorf("texts entry %q has unknown flag %q", e.name, w)
			}
		}
		if e.has("capacity") && !e.has("gated") {
			t.Errorf("texts entry %q is a capacity rule but not gated", e.name)
		}
	}
	if want := metrics.MetricNames(); !slices.Equal(names, want) {
		t.Errorf("texts names = %v\nwant metrics.MetricNames() = %v", names, want)
	}
}

// TestExplainCapacityOverCeiling checks each capacity metric's explanation
// says what happens over the ceiling (SPEC.md 8.1): a crossing fails and
// growth past it is held by over_max_delta.
func TestExplainCapacityOverCeiling(t *testing.T) {
	for _, name := range []string{"sloc", "largest_file_sloc", "tokens_est", "exported_symbols", "internal_imports"} {
		t.Run(name, func(t *testing.T) {
			e, ok := Explain(name)
			if !ok {
				t.Fatalf("Explain(%q) has no entry", name)
			}
			for _, want := range []string{"crossing the max", "over_max_delta", "split"} {
				if !strings.Contains(e.Evidence, want) {
					t.Errorf("Explain(%q).Evidence lacks %q: %s", name, want, e.Evidence)
				}
			}
		})
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

// TestExplainCrossPackageAndOpacity checks that the four fields the Go
// extractor computes without a default threshold say so, and that
// dup_blocks_cross_pkg, which the default gates on the module row, says
// that.
func TestExplainCrossPackageAndOpacity(t *testing.T) {
	tests := map[string]string{
		"uses_cgo":             "not gated",
		"uses_reflect":         "not gated",
		"generated_files":      "not gated",
		"tokens_est_generated": "not gated",
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
	if e, _ := Explain("dup_blocks_cross_pkg"); !e.Gated || !strings.Contains(e.Evidence, "The default gates the module row") {
		t.Errorf("dup_blocks_cross_pkg = gated %v, evidence %q; want gated on the module row", e.Gated, e.Evidence)
	}
	if e, _ := Explain("dup_blocks_cross_pkg"); !strings.Contains(e.Definition, `"`+metrics.ModuleRowID+`"`) {
		t.Errorf("dup_blocks_cross_pkg definition does not name the module row: %s", e.Definition)
	}
	if e, _ := Explain("dup_blocks_cross_pkg"); !strings.Contains(e.Definition, "gated on the module row only") {
		t.Errorf("dup_blocks_cross_pkg definition does not say it is gated on the module row only: %s", e.Definition)
	}
}

// TestExplainGeneratedExcluded checks that every metric generated files
// stay out of says so, and that the ones counting them do not.
func TestExplainGeneratedExcluded(t *testing.T) {
	excluded := []string{
		"sloc", "largest_file_sloc", "tokens_est", "tokens_est_with_tests", "exported_symbols",
		"globals", "init_funcs", "max_nesting", "cognitive_total", "cognitive_p90", "func_count",
		"dup_blocks", "dup_blocks_code", "duplication_pct", "untested_exports", "duplicated_lines", "abstractness",
	}
	for _, name := range metrics.MetricNames() {
		e, _ := Explain(name)
		if got, want := strings.HasSuffix(e.Definition, generatedExcluded), slices.Contains(excluded, name); got != want {
			t.Errorf("Explain(%q) says generated files are left out: %v, want %v", name, got, want)
		}
	}
	if e, _ := Explain("files"); !strings.Contains(e.Definition, "generated files included") {
		t.Errorf("files definition does not say generated files count: %s", e.Definition)
	}
}

// TestExplainFanIn checks that fan_in's evidence matches SPEC.md section 4:
// unproven, reported for ranking, and outside the estimate formula.
func TestExplainFanIn(t *testing.T) {
	e, ok := Explain("fan_in")
	if !ok {
		t.Fatal("Explain(fan_in) has no entry")
	}
	if e.Gated {
		t.Error("Explain(fan_in).Gated = true, want false")
	}
	for _, want := range []string{"no direct study", "Unproven; reported for ranking", "not in its formula"} {
		if !strings.Contains(e.Evidence, want) {
			t.Errorf("Explain(fan_in).Evidence lacks %q: %s", want, e.Evidence)
		}
	}
	for _, stale := range []string{"strongest predictor", "drives the rebuild estimate"} {
		if strings.Contains(e.Evidence, stale) {
			t.Errorf("Explain(fan_in).Evidence still claims %q: %s", stale, e.Evidence)
		}
	}
}
