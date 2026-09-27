package score

import (
	"math"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

func approx(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.6f, want %.6f (tol %g)", name, got, want, tol)
	}
}

func termTokens(r Rebuild) map[string]float64 {
	out := make(map[string]float64, len(r.Terms))
	for _, tm := range r.Terms {
		out[tm.Name] = tm.Tokens
	}
	return out
}

func TestEstimateZeroMetrics(t *testing.T) {
	t.Parallel()

	r := Estimate(metrics.RawMetrics{HasTests: true}, validParams())
	if r.RebuildTokens != 0 || r.AgentPasses != 0 || r.HumanDays != 0 {
		t.Fatalf("Estimate(zero) = %+v, want all zero", r)
	}
}

func TestEstimateTermsOrder(t *testing.T) {
	t.Parallel()

	r := Estimate(metrics.RawMetrics{}, validParams())
	want := []string{TermVolume, TermSpec, TermContract, TermUnspecified, TermHidden}
	got := make([]string, 0, len(r.Terms))
	for _, tm := range r.Terms {
		got = append(got, tm.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("term order = %v, want %v", got, want)
	}
}

func TestEstimateTermsInIsolation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		m          metrics.RawMetrics
		term       string
		want       float64
		wantDetail string
	}{
		{
			name: "volume", term: TermVolume, want: 750,
			m:          metrics.RawMetrics{TokensEst: 1000, TokensEstWithTests: 1000, DuplicationPct: 25},
			wantDetail: "tokens_est=1000 duplication_pct=25",
		},
		{
			name: "spec", term: TermSpec, want: 400,
			m:          metrics.RawMetrics{TokensEstWithTests: 400, TestFuncs: 3},
			wantDetail: "tokens_est_with_tests=400 tokens_est=0 test_funcs=3",
		},
		{
			name: "contract", term: TermContract, want: 7 * 40,
			m:          metrics.RawMetrics{ExportedSymbols: 7, FanIn: 5},
			wantDetail: "exported_symbols=7 fan_in=5",
		},
		{
			name: "unspecified", term: TermUnspecified, want: 3 * 800,
			m:          metrics.RawMetrics{UntestedExports: 3},
			wantDetail: "untested_exports=3",
		},
		{
			name: "hidden", term: TermHidden, want: (2 + 1) * 400,
			m:          metrics.RawMetrics{Globals: 2, InitFuncs: 1},
			wantDetail: "globals=2 init_funcs=1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := Estimate(tt.m, validParams())
			for _, tm := range r.Terms {
				want := 0.0
				if tm.Name == tt.term {
					want = tt.want
					if tm.Detail != tt.wantDetail {
						t.Errorf("%s detail = %q, want %q", tm.Name, tm.Detail, tt.wantDetail)
					}
				}
				approx(t, tm.Name, tm.Tokens, want, 1e-9)
			}
			approx(t, "rebuild_tokens", r.RebuildTokens, tt.want, 1e-9)
		})
	}
}

func TestEstimateFanInDoesNotChangeTokens(t *testing.T) {
	t.Parallel()

	cfg := validParams()
	a := Estimate(metrics.RawMetrics{ExportedSymbols: 4}, cfg)
	b := Estimate(metrics.RawMetrics{ExportedSymbols: 4, FanIn: 30}, cfg)
	if a.RebuildTokens != b.RebuildTokens || a.HumanDays != b.HumanDays {
		t.Fatalf("fan_in changed the estimate: %+v vs %+v", a, b)
	}
}

func TestEstimateAgentPasses(t *testing.T) {
	t.Parallel()

	cfg := validParams()
	tests := []struct {
		name      string
		tokens    int
		wantRatio float64
		want      float64
	}{
		{name: "below knee", tokens: 12500, wantRatio: 0.5, want: 0.5},
		{name: "at knee", tokens: 25000, wantRatio: 1, want: 1},
		{name: "twice budget", tokens: 50000, wantRatio: 2, want: math.Pow(2, 1.3)},
		{name: "four times budget", tokens: 100000, wantRatio: 4, want: math.Pow(4, 1.3)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := Estimate(metrics.RawMetrics{TokensEst: tt.tokens, TokensEstWithTests: tt.tokens}, cfg)
			approx(t, "ratio", r.Ratio, tt.wantRatio, 1e-12)
			approx(t, "agent_passes", r.AgentPasses, tt.want, 1e-12)
		})
	}
}

func TestAgentPassesRounded(t *testing.T) {
	t.Parallel()

	tests := []struct{ in, want float64 }{
		{0, 0}, {0.04, 0}, {0.05, 0.1}, {1, 1}, {math.Pow(2, 1.3), 2.5}, {1.1803, 1.2},
	}
	for _, tt := range tests {
		if got := (Rebuild{AgentPasses: tt.in}).AgentPassesRounded(); got != tt.want {
			t.Errorf("AgentPassesRounded(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestEstimateDuplicationHalvesVolumeOnly(t *testing.T) {
	t.Parallel()

	cfg := validParams()
	base := metrics.RawMetrics{
		TokensEst: 8000, TokensEstWithTests: 11000, TestFuncs: 6,
		ExportedSymbols: 10, FanIn: 2, UntestedExports: 3, Globals: 1, InitFuncs: 1,
		SLOC: 900,
	}
	doubled := base
	doubled.DuplicationPct = 50

	a, b := termTokens(Estimate(base, cfg)), termTokens(Estimate(doubled, cfg))
	approx(t, "volume", b[TermVolume], a[TermVolume]/2, 1e-9)
	for _, name := range []string{TermSpec, TermContract, TermUnspecified, TermHidden} {
		if a[name] != b[name] {
			t.Errorf("%s changed from %v to %v", name, a[name], b[name])
		}
	}
}

// TestEstimateWorkedExample is a hand-computed example. SPEC.md 7.2 has no
// worked example of its own, so every intermediate value is written out here
// against the default parameters.
func TestEstimateWorkedExample(t *testing.T) {
	t.Parallel()

	m := metrics.RawMetrics{
		SLOC:               1200,
		TokensEst:          10000,
		TokensEstWithTests: 16000,
		DuplicationPct:     20,
		ExportedSymbols:    30,
		FanIn:              4,
		UntestedExports:    15,
		Globals:            2,
		InitFuncs:          1,
		TestFuncs:          12,
		HasTests:           true,
	}
	r := Estimate(m, validParams())
	got := termTokens(r)

	// volume      = 10000 * (1 - 20/100)  = 8000
	// spec        = 16000 - 10000         = 6000
	// contract    = 30 * 40               = 1200
	// unspecified = 15 * 800              = 12000
	// hidden      = (2 + 1) * 400         = 1200
	// rebuild_tokens                      = 28400
	approx(t, "volume", got[TermVolume], 8000, 1e-4)
	approx(t, "spec", got[TermSpec], 6000, 1e-4)
	approx(t, "contract", got[TermContract], 1200, 1e-4)
	approx(t, "unspecified", got[TermUnspecified], 12000, 1e-4)
	approx(t, "hidden", got[TermHidden], 1200, 1e-4)
	approx(t, "rebuild_tokens", r.RebuildTokens, 28400, 1e-4)

	// r = 28400 / 25000 = 1.136; agent_passes = 1.136 ^ 1.3 = 1.1803
	approx(t, "ratio", r.Ratio, 1.136, 1e-4)
	approx(t, "agent_passes", r.AgentPasses, 1.1803, 1e-4)
	if got := r.AgentPassesRounded(); got != 1.2 {
		t.Errorf("AgentPassesRounded() = %v, want 1.2", got)
	}

	// untested_ratio = 15 / 30 = 0.5
	// kloc_eff = (1200 * 0.8 / 1000) * (1 + 0.5 * 0.5) = 0.96 * 1.25 = 1.2
	// person_months = 2.4 * 1.2 ^ 1.05 = 2.9064
	// human_days = 2.9064 * 19 = 55.2211
	approx(t, "human_days", r.HumanDays, 55.2211, 1e-4)
}

// TestEstimateCocomo checks the human estimate against COCOMO basic organic
// mode, the model scc publishes: 2.4 person-months for 1 KLOC and
// 2.4 * 10^1.05 = 26.93 for 10 KLOC.
func TestEstimateCocomo(t *testing.T) {
	t.Parallel()

	cfg := validParams()
	tests := []struct {
		sloc         int
		personMonths float64
	}{
		{sloc: 1000, personMonths: 2.4},
		{sloc: 10000, personMonths: 26.93},
	}
	for _, tt := range tests {
		r := Estimate(metrics.RawMetrics{SLOC: tt.sloc}, cfg)
		approx(t, "person_months", r.HumanDays/cfg.DaysPerMonth, tt.personMonths, 1e-2)
	}
}

func TestEstimateUntestedRatio(t *testing.T) {
	t.Parallel()

	cfg := validParams()
	full := Estimate(metrics.RawMetrics{SLOC: 1000, ExportedSymbols: 4, UntestedExports: 4}, cfg)
	// untested_ratio 1 -> kloc_eff 1.5.
	approx(t, "human_days", full.HumanDays, 2.4*math.Pow(1.5, 1.05)*19, 1e-9)
	// No exported symbols: max(exported_symbols, 1) avoids a division by
	// zero, and the ratio is clamped to 1.
	clamped := Estimate(metrics.RawMetrics{SLOC: 1000, UntestedExports: 3}, cfg)
	approx(t, "human_days clamped", clamped.HumanDays, full.HumanDays, 1e-9)
}

func TestEstimateIgnoresV1Fields(t *testing.T) {
	t.Parallel()

	base := metrics.RawMetrics{
		SLOC: 500, TokensEst: 5000, TokensEstWithTests: 7000,
		ExportedSymbols: 8, UntestedExports: 2, Globals: 1, HasTests: true,
	}
	withV1 := base
	ratio := 0.7
	dup, gen := 4, 2
	yes := true
	withV1.Instability = &ratio
	withV1.Abstractness = &ratio
	withV1.MainSequenceDistance = &ratio
	withV1.DupBlocksCrossPkg = &dup
	withV1.UsesCgo = &yes
	withV1.UsesReflect = &yes
	withV1.GeneratedFiles = &gen
	withV1.ChangedFuncCognitiveMax = &dup

	cfg := validParams()
	if a, b := Estimate(base, cfg), Estimate(withV1, cfg); !reflect.DeepEqual(a, b) {
		t.Fatalf("v1 fields other than coverage_pct changed the estimate:\n%+v\n%+v", a, b)
	}
}

func TestEstimateCoverageScalesUnspecified(t *testing.T) {
	t.Parallel()

	// The SPEC.md 7.2 worked example: unspecified = 15 * 800 = 12000 without
	// coverage, the step penalty.
	base := metrics.RawMetrics{
		SLOC: 1200, TokensEst: 10000, TokensEstWithTests: 16000, DuplicationPct: 20,
		ExportedSymbols: 30, UntestedExports: 15, Globals: 2, InitFuncs: 1, HasTests: true,
	}
	tests := []struct {
		name        string
		coverage    *float64
		unspecified float64
		detail      string
	}{
		{name: "null applies the step penalty", coverage: nil, unspecified: 12000, detail: "untested_exports=15"},
		{name: "zero keeps the full term", coverage: new(0.0), unspecified: 12000, detail: "untested_exports=15 coverage_pct=0"},
		{name: "partial scales the term", coverage: new(62.5), unspecified: 4500, detail: "untested_exports=15 coverage_pct=62.5"},
		{name: "full removes the term", coverage: new(100.0), unspecified: 0, detail: "untested_exports=15 coverage_pct=100"},
	}
	cfg := validParams()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := base
			m.CoveragePct = tc.coverage
			r := Estimate(m, cfg)
			tm := r.Terms[3]
			approx(t, "unspecified", tm.Tokens, tc.unspecified, 1e-9)
			if tm.Detail != tc.detail {
				t.Errorf("Detail = %q, want %q", tm.Detail, tc.detail)
			}
			approx(t, "rebuild_tokens", r.RebuildTokens, 16400+tc.unspecified, 1e-9)
			// human_days reads untested_exports, not coverage.
			approx(t, "human_days", r.HumanDays, Estimate(base, cfg).HumanDays, 1e-9)
		})
	}
}

func TestEstimateProperty(t *testing.T) {
	t.Parallel()

	cfg := validParams()
	rng := rand.New(rand.NewPCG(1, 17))
	finiteNonNeg := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }
	for i := range 1000 {
		tokens := rng.IntN(500000)
		exported := rng.IntN(300)
		m := metrics.RawMetrics{
			SLOC:               rng.IntN(50000),
			TokensEst:          tokens,
			TokensEstWithTests: tokens + rng.IntN(500000),
			DuplicationPct:     rng.Float64() * 100,
			ExportedSymbols:    exported,
			FanIn:              rng.IntN(100),
			UntestedExports:    rng.IntN(exported + 1),
			Globals:            rng.IntN(50),
			InitFuncs:          rng.IntN(5),
			TestFuncs:          rng.IntN(200),
		}
		m.HasTests = m.TestFuncs > 0
		r := Estimate(m, cfg)
		for name, v := range map[string]float64{
			"rebuild_tokens": r.RebuildTokens, "ratio": r.Ratio,
			"agent_passes": r.AgentPasses, "human_days": r.HumanDays,
		} {
			if !finiteNonNeg(v) {
				t.Fatalf("iteration %d: %s = %v for %+v", i, name, v, m)
			}
		}
		for _, tm := range r.Terms {
			if !finiteNonNeg(tm.Tokens) {
				t.Fatalf("iteration %d: term %s = %v for %+v", i, tm.Name, tm.Tokens, m)
			}
		}
		if r.Ratio > 1 && r.AgentPasses < r.Ratio {
			t.Fatalf("iteration %d: agent_passes %v < ratio %v", i, r.AgentPasses, r.Ratio)
		}
	}
}
