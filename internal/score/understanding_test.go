package score

import (
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

func TestBandBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    float64
		cuts [4]float64
		want int
	}{
		{"cognitive 4 very high", 4, suCuts(cutCognitive), 4},
		{"cognitive 5 high", 5, suCuts(cutCognitive), 3},
		{"cognitive 10 high", 10, suCuts(cutCognitive), 3},
		{"cognitive 11 nominal", 11, suCuts(cutCognitive), 2},
		{"cognitive 20 nominal", 20, suCuts(cutCognitive), 2},
		{"cognitive 21 low", 21, suCuts(cutCognitive), 1},
		{"cognitive 40 low", 40, suCuts(cutCognitive), 1},
		{"cognitive 41 very low", 41, suCuts(cutCognitive), 0},
		{"dup 0 very high", 0, suCuts(cutDup), 4},
		{"dup 1 high", 1, suCuts(cutDup), 3},
		{"dup 2 high", 2, suCuts(cutDup), 3},
		{"dup 3 nominal", 3, suCuts(cutDup), 2},
		{"dup 5 nominal", 5, suCuts(cutDup), 2},
		{"dup 6 low", 6, suCuts(cutDup), 1},
		{"dup 10 low", 10, suCuts(cutDup), 1},
		{"dup 11 very low", 11, suCuts(cutDup), 0},
		{"missing 0.05 very high", 0.05, suCuts(cutMissing), 4},
		{"missing 0.06 high", 0.06, suCuts(cutMissing), 3},
		{"missing 0.15 high", 0.15, suCuts(cutMissing), 3},
		{"missing 0.16 nominal", 0.16, suCuts(cutMissing), 2},
		{"missing 0.35 nominal", 0.35, suCuts(cutMissing), 2},
		{"missing 0.36 low", 0.36, suCuts(cutMissing), 1},
		{"missing 0.6 low", 0.6, suCuts(cutMissing), 1},
		{"missing 0.61 very low", 0.61, suCuts(cutMissing), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := band(tt.v, tt.cuts); got != tt.want {
				t.Errorf("band(%v) = %d, want %d", tt.v, got, tt.want)
			}
		})
	}
}

func TestUnderstanding(t *testing.T) {
	t.Parallel()

	ptr := func(n int) *int { return &n }
	tests := []struct {
		name string
		m    metrics.RawMetrics
		want float64
	}{
		{
			// The table's floor: every sub-score very high.
			name: "every export tested and documented",
			m:    metrics.RawMetrics{ExportedSymbols: 20, DocumentedExports: ptr(20), FuncCount: 10, CognitiveP90: 4},
			want: 10,
		},
		{
			// The table's ceiling: clarity and self-description very low,
			// structure nominal, (2 + 0 + 0) / 3 rounds down to 0.
			name: "no export tested or documented",
			m: metrics.RawMetrics{ExportedSymbols: 20, UntestedExports: 20, DocumentedExports: ptr(0),
				FuncCount: 10, CognitiveP90: 15, DupBlocks: 4},
			want: 50,
		},
		{
			// Structure very high lifts the mean to 4 / 3, low.
			name: "no export tested or documented, simple",
			m:    metrics.RawMetrics{ExportedSymbols: 20, UntestedExports: 20, DocumentedExports: ptr(0)},
			want: 40,
		},
		{
			// (4 + 4 + 3) / 3 rounds down to 3, toward the worse rating.
			name: "one copy",
			m:    metrics.RawMetrics{ExportedSymbols: 20, DocumentedExports: ptr(20), DupBlocks: 1},
			want: 20,
		},
		{
			// Structure takes the worse of cognitive_p90 and dup_blocks:
			// (0 + 4 + 4) / 3 rounds down to 2.
			name: "complex, no copies",
			m:    metrics.RawMetrics{ExportedSymbols: 20, DocumentedExports: ptr(20), FuncCount: 5, CognitiveP90: 41},
			want: 30,
		},
		{
			// documented_exports not measured (TypeScript): nominal, so
			// (4 + 4 + 2) / 3 = 3.
			name: "self-description unmeasured",
			m:    metrics.RawMetrics{ExportedSymbols: 20},
			want: 20,
		},
		{
			// No exports: nothing untested or undocumented.
			name: "no exports",
			m:    metrics.RawMetrics{DocumentedExports: ptr(0)},
			want: 10,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := understanding(&tt.m); got != tt.want {
				t.Errorf("SU = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestUnderstandingTestingLowersSU checks that testing one more export
// never raises SU, the property 7.5's adding-tests invariant needs.
func TestUnderstandingTestingLowersSU(t *testing.T) {
	t.Parallel()

	for exports := 1; exports <= 40; exports++ {
		for untested := 1; untested <= exports; untested++ {
			m := metrics.RawMetrics{ExportedSymbols: exports, UntestedExports: untested}
			tested := m
			tested.UntestedExports--
			if a, b := understanding(&m), understanding(&tested); b > a {
				t.Fatalf("%d of %d untested: testing one raised SU from %v to %v", untested, exports, a, b)
			}
		}
	}
}

func TestUnfm(t *testing.T) {
	t.Parallel()

	want := []float64{0, 0.2, 0.4, 0.6, 0.8, 1}
	for i, k := range UnfmKeys() {
		if got, ok := unfm(k); !ok || got != want[i] {
			t.Errorf("unfm(%q) = %v, %v; want %v", k, got, ok, want[i])
		}
	}
	for _, name := range []string{"", "unfamiliar"} {
		if _, ok := unfm(name); ok {
			t.Errorf("unfm accepted the unknown rating %q", name)
		}
		if err := (Cocomo{UNFM: name}).validate(); err == nil {
			t.Errorf("validate accepted the unknown unfm %q", name)
		}
	}
}

// TestEffectiveKLOCRange checks the inflation's range under the shipped
// unfamiliarity, 1.2 to 2.0, and that completely_familiar removes it.
func TestEffectiveKLOCRange(t *testing.T) {
	t.Parallel()

	ptr := func(n int) *int { return &n }
	best := metrics.RawMetrics{SLOC: 1000, ExportedSymbols: 2, DocumentedExports: ptr(2)}
	worst := metrics.RawMetrics{SLOC: 1000, ExportedSymbols: 2, UntestedExports: 2, DocumentedExports: ptr(0),
		FuncCount: 2, CognitiveP90: 50}
	c := Cocomo{UNFM: "completely_unfamiliar"}
	approx(t, "best", effectiveKLOC(&best, &c, 1), 1.2, 1e-12)
	approx(t, "worst", effectiveKLOC(&worst, &c, 1), 2.0, 1e-12)
	c.UNFM = "completely_familiar"
	approx(t, "familiar", effectiveKLOC(&worst, &c, 1), 1.0, 1e-12)
	c.UNFM = "somewhat_familiar"
	// 1 + 0.02 * 50 * 0.4.
	approx(t, "somewhat familiar", effectiveKLOC(&worst, &c, 0.5), 0.5*1.4, 1e-12)
}
