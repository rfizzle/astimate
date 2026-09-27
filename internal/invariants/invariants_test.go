package invariants

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// envConfig names the environment variable that points the suite at a
// configuration file other than the embedded default.
const envConfig = "ASTIMATE_CONFIG"

// seed fixes the random draws so a failure reproduces; it is logged by every
// property test.
const seed = 20260927

// Iteration counts. monotoneIterations is the count SPEC.md 7.5's property
// test is specified with; the gate properties draw enough metric records to
// reach every rule kind many times over.
const (
	monotoneIterations   = 1000
	gateIterations       = 1000
	thresholdSets        = 200
	drawsPerThresholdSet = 20
)

// relTol absorbs float rounding when comparing two estimates that are equal
// in exact arithmetic.
const relTol = 1e-12

// configUnderTest returns the configuration named by ASTIMATE_CONFIG, or the
// embedded default when it is unset, and a description of which was used.
func configUnderTest() (*config.Config, string, error) {
	if path := os.Getenv(envConfig); path != "" {
		cfg, err := config.Load(path)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", envConfig, err)
		}
		return cfg, path, nil
	}
	cfg, err := config.Parse(config.Default())
	if err != nil {
		return nil, "", fmt.Errorf("embedded default: %w", err)
	}
	return cfg, "embedded default", nil
}

// loadConfig is configUnderTest for a test: it fails the test on error and
// logs which configuration the test runs under.
func loadConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, source, err := configUnderTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("config: %s (config_version %s)", source, cfg.Version)
	return cfg
}

func TestConfigOverrideViaEnvironment(t *testing.T) {
	t.Run("unset uses the embedded default", func(t *testing.T) {
		t.Setenv(envConfig, "")
		want, err := config.Parse(config.Default())
		if err != nil {
			t.Fatal(err)
		}
		got, source, err := configUnderTest()
		if err != nil {
			t.Fatal(err)
		}
		if source != "embedded default" || got.Version != want.Version {
			t.Errorf("source %q version %q, want embedded default %q", source, got.Version, want.Version)
		}
	})
	t.Run("set loads the named file", func(t *testing.T) {
		data := strings.Replace(string(config.Default()), "config_version: default-uncalibrated-1",
			"config_version: invariants-override", 1)
		if !strings.Contains(data, "invariants-override") {
			t.Fatal("default config no longer carries the expected config_version line")
		}
		path := filepath.Join(t.TempDir(), "astimate.yaml")
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv(envConfig, path)
		got, source, err := configUnderTest()
		if err != nil {
			t.Fatal(err)
		}
		if source != path || got.Version != "invariants-override" {
			t.Errorf("source %q version %q, want %q invariants-override", source, got.Version, path)
		}
	})
	t.Run("a bad file is an error", func(t *testing.T) {
		t.Setenv(envConfig, filepath.Join(t.TempDir(), "missing.yaml"))
		if _, _, err := configUnderTest(); err == nil || !strings.Contains(err.Error(), envConfig) {
			t.Errorf("err = %v, want an error naming %s", err, envConfig)
		}
	})
}

// extractorOptions carries the configuration's extraction settings to the Go
// extractor, so a calibrated chars_per_token or duplication rule is measured
// the way it would be in a real run.
func extractorOptions(cfg *config.Config) []golang.Option {
	return []golang.Option{
		golang.WithCharsPerToken(cfg.CharsPerToken),
		golang.WithDupMinTokens(cfg.DupMinTokens),
		golang.WithDupIgnoreLiteralOnly(cfg.DupIgnoreLiteralOnly),
		golang.WithDupFoldSigns(cfg.DupFoldSigns),
	}
}

// TestStdlibTiers checks the first invariant of SPEC.md 7.5: the standard
// library's errors package is ONE_PASS and net/http is PARTITION. A package
// the toolchain cannot load is skipped.
func TestStdlibTiers(t *testing.T) {
	cfg := loadConfig(t)
	tests := []struct {
		importPath string
		want       score.Tier
	}{
		{"errors", score.TierOnePass},
		{"net/http", score.TierPartition},
	}
	for _, tt := range tests {
		t.Run(tt.importPath, func(t *testing.T) {
			if testing.Short() && tt.importPath == "net/http" {
				t.Skip("loads net/http")
			}
			m, err := golang.ExtractStdlib(t.Context(), tt.importPath, extractorOptions(cfg)...)
			if err != nil {
				t.Skipf("loading stdlib %s: %v", tt.importPath, err)
			}
			est := score.Estimate(m, cfg.Rebuild)
			got := score.TierOf(est.AgentPasses, cfg.Rebuild.Tiers)
			t.Logf("%s: rebuild_tokens %.0f agent_passes %.4f tier %s", tt.importPath, est.RebuildTokens, est.AgentPasses, got)
			if got != tt.want {
				t.Errorf("%s tier = %s (agent_passes %.4f), want %s", tt.importPath, got, est.AgentPasses, tt.want)
			}
		})
	}
}

// TestSmallTestedPackageIsOnePass checks the second invariant of SPEC.md 7.5:
// zero fan-in, tests present, no duplication and under 5k tokens is ONE_PASS.
// The test code is as large as the code it tests, and every export is tested.
func TestSmallTestedPackageIsOnePass(t *testing.T) {
	cfg := loadConfig(t)
	m := metrics.RawMetrics{
		Files:              4,
		SLOC:               450,
		LargestFileSLOC:    180,
		TokensEst:          4999,
		TokensEstWithTests: 2 * 4999,
		StdlibImports:      5,
		ExportedSymbols:    20,
		MaxNesting:         3,
		CognitiveTotal:     60,
		CognitiveP90:       8,
		FuncCount:          25,
		TestFiles:          3,
		TestFuncs:          18,
		HasTests:           true,
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	est := score.Estimate(m, cfg.Rebuild)
	got := score.TierOf(est.AgentPasses, cfg.Rebuild.Tiers)
	t.Logf("rebuild_tokens %.0f agent_passes %.4f tier %s", est.RebuildTokens, est.AgentPasses, got)
	if got != score.TierOnePass {
		t.Errorf("tier = %s (agent_passes %.4f), want %s", got, est.AgentPasses, score.TierOnePass)
	}
}

// randomMetrics draws a v0 record that passes RawMetrics.Validate, with
// magnitudes spanning small packages to ones several times the default
// context budget.
func randomMetrics(rng *rand.Rand) metrics.RawMetrics {
	sloc := rng.IntN(20000)
	tokens := rng.IntN(150000)
	exported := rng.IntN(300)
	testFuncs := rng.IntN(3) * rng.IntN(200)
	m := metrics.RawMetrics{
		Files:              1 + rng.IntN(60),
		SLOC:               sloc,
		LargestFileSLOC:    rng.IntN(sloc + 1),
		TokensEst:          tokens,
		TokensEstWithTests: tokens + rng.IntN(150000),
		InternalImports:    rng.IntN(20),
		ExternalImports:    rng.IntN(10),
		StdlibImports:      rng.IntN(30),
		FanIn:              rng.IntN(50),
		FanInTests:         rng.IntN(10),
		ExportedSymbols:    exported,
		Globals:            rng.IntN(30),
		InitFuncs:          rng.IntN(4),
		MaxNesting:         rng.IntN(10),
		CognitiveTotal:     rng.IntN(3000),
		CognitiveP90:       rng.IntN(40),
		FuncCount:          rng.IntN(600),
		DupBlocks:          rng.IntN(20),
		DuplicationPct:     math.Round(rng.Float64()*1000) / 10,
		TestFiles:          rng.IntN(20),
		TestFuncs:          testFuncs,
		HasTests:           testFuncs > 0,
		UntestedExports:    rng.IntN(exported + 1),
	}
	if rng.IntN(2) == 0 {
		ratio := rng.Float64()
		cross := rng.IntN(5)
		cgo, reflect := rng.IntN(2) == 0, rng.IntN(2) == 0
		gen := rng.IntN(3)
		cov := math.Round(rng.Float64()*1000) / 10
		changed := rng.IntN(30)
		m.ConcreteParamRatio, m.DupBlocksCrossPkg = &ratio, &cross
		m.UsesCgo, m.UsesReflect, m.GeneratedFiles = &cgo, &reflect, &gen
		m.CoveragePct, m.ChangedFuncCognitiveMax = &cov, &changed
	}
	return m
}

// direction is the way a mutation may move agent_passes.
type direction int

const (
	// neverLowers means agent_passes after the mutation is at least before.
	neverLowers direction = iota
	// neverRaises means agent_passes after the mutation is at most before.
	neverRaises
)

// mutation increases one input of the estimate by a random positive amount.
type mutation struct {
	name  string
	dir   direction
	apply func(m *metrics.RawMetrics, rng *rand.Rand)
}

// monotoneMutations are the directions SPEC.md 7.5 fixes. Raising tokens_est
// raises tokens_est_with_tests by as much, so the spec term, their difference,
// stays put and only the volume term moves.
func monotoneMutations() []mutation {
	return []mutation{
		{"tokens_est", neverLowers, func(m *metrics.RawMetrics, rng *rand.Rand) {
			d := 1 + rng.IntN(60000)
			m.TokensEst += d
			m.TokensEstWithTests += d
		}},
		{"exported_symbols", neverLowers, func(m *metrics.RawMetrics, rng *rand.Rand) { m.ExportedSymbols += 1 + rng.IntN(200) }},
		{"untested_exports", neverLowers, func(m *metrics.RawMetrics, rng *rand.Rand) { m.UntestedExports += 1 + rng.IntN(200) }},
		{"globals", neverLowers, func(m *metrics.RawMetrics, rng *rand.Rand) { m.Globals += 1 + rng.IntN(20) }},
		{"init_funcs", neverLowers, func(m *metrics.RawMetrics, rng *rand.Rand) { m.InitFuncs += 1 + rng.IntN(5) }},
		{"duplication_pct", neverRaises, func(m *metrics.RawMetrics, rng *rand.Rand) {
			m.DuplicationPct = math.Min(100, m.DuplicationPct+0.1+rng.Float64()*(100-m.DuplicationPct))
		}},
	}
}

// monotone runs the monotonicity property of SPEC.md 7.5 for n random records
// under p and returns an error describing the first counterexample, or nil.
// It returns an error rather than failing a test so a deliberately broken p
// can be shown to fail it.
func monotone(rng *rand.Rand, p score.RebuildParams, n int) error {
	muts := monotoneMutations()
	for i := range n {
		m := randomMetrics(rng)
		before := score.Estimate(m, p).AgentPasses
		for _, mu := range muts {
			next := m
			mu.apply(&next, rng)
			if err := next.Validate(); err != nil {
				return fmt.Errorf("iteration %d: %s mutation produced invalid metrics: %w", i, mu.name, err)
			}
			after := score.Estimate(next, p).AgentPasses
			tol := relTol * math.Max(1, math.Abs(before))
			switch {
			case mu.dir == neverLowers && after < before-tol:
				return fmt.Errorf("iteration %d: raising %s lowered agent_passes from %v to %v (%+v -> %+v)",
					i, mu.name, before, after, m, next)
			case mu.dir == neverRaises && after > before+tol:
				return fmt.Errorf("iteration %d: raising %s raised agent_passes from %v to %v (%+v -> %+v)",
					i, mu.name, before, after, m, next)
			}
		}
	}
	return nil
}

func TestMonotonicity(t *testing.T) {
	cfg := loadConfig(t)
	t.Logf("seed %d, %d iterations", seed, monotoneIterations)
	if err := monotone(rand.New(rand.NewPCG(seed, seed)), cfg.Rebuild, monotoneIterations); err != nil {
		t.Error(err)
	}
}

// TestMonotonicityCatchesBrokenEstimate forces a superlinear exponent below 1
// past RebuildParams.Validate and checks the property rejects it. The
// exponent must be negative to break monotonicity: any exponent in (0, 1)
// still makes r^e increasing in r and continuous at 1, so it bends the curve
// the wrong way without ever lowering agent_passes.
func TestMonotonicityCatchesBrokenEstimate(t *testing.T) {
	cfg := loadConfig(t)
	for _, exp := range []float64{-0.5, -1} {
		t.Run(fmt.Sprint(exp), func(t *testing.T) {
			p := cfg.Rebuild
			p.SuperlinearExponent = exp
			if p.Validate() == nil {
				t.Fatalf("Validate accepted superlinear_exponent %v; the case must bypass it", exp)
			}
			err := monotone(rand.New(rand.NewPCG(seed, seed)), p, monotoneIterations)
			if err == nil {
				t.Fatalf("monotonicity property passed with superlinear_exponent %v, want a counterexample", exp)
			}
			t.Logf("rejected as expected: %v", firstLine(err))
		})
	}
}

// firstLine returns the text of err up to its first parenthesized record
// dump, to keep logs short.
func firstLine(err error) string {
	s, _, _ := strings.Cut(err.Error(), " (")
	return s
}

// TestAddingTests covers the third clause of SPEC.md 7.5, "adding tests never
// raises it". Under the 7.2 formula the spec term charges test tokens, so
// adding test tokens alone raises agent_passes; the clause holds for
// human_days, which has no test-token term and falls as tests cover exports.
// The test asserts what holds: adding test tokens changes only the spec term,
// by exactly the tokens added, and adding tests never raises human_days.
func TestAddingTests(t *testing.T) {
	cfg := loadConfig(t)
	rng := rand.New(rand.NewPCG(seed, seed))
	t.Logf("seed %d, %d iterations", seed, monotoneIterations)
	for i := range monotoneIterations {
		m := randomMetrics(rng)
		before := score.Estimate(m, cfg.Rebuild)

		next := m
		added := 1 + rng.IntN(40000)
		next.TokensEstWithTests += added
		next.TestFuncs += 1 + rng.IntN(20)
		next.TestFiles++
		next.HasTests = true
		after := score.Estimate(next, cfg.Rebuild)
		for j, term := range after.Terms {
			want := before.Terms[j].Tokens
			if term.Name == score.TermSpec {
				want += float64(added)
			}
			if term.Tokens != want {
				t.Fatalf("iteration %d: adding %d test tokens moved %s from %v to %v, want %v",
					i, added, term.Name, before.Terms[j].Tokens, term.Tokens, want)
			}
		}
		if after.HumanDays != before.HumanDays {
			t.Fatalf("iteration %d: adding test tokens moved human_days from %v to %v", i, before.HumanDays, after.HumanDays)
		}

		// Tests that also cover previously untested exports.
		next.UntestedExports -= rng.IntN(next.UntestedExports + 1)
		covered := score.Estimate(next, cfg.Rebuild)
		if covered.HumanDays > before.HumanDays*(1+relTol) {
			t.Fatalf("iteration %d: adding tests raised human_days from %v to %v (%+v -> %+v)",
				i, before.HumanDays, covered.HumanDays, m, next)
		}
	}
}

// TestSpecTermConflict records, with the SPEC.md 7.2 worked example, that
// adding test tokens raises agent_passes under the 7.2 formula, which is
// what makes the 7.5 clause "adding tests never raises it" unsatisfiable for
// agent_passes as written. It runs under the default parameters only, since
// the worked example is defined by them.
func TestSpecTermConflict(t *testing.T) {
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	m := metrics.RawMetrics{
		SLOC: 1200, LargestFileSLOC: 400, TokensEst: 10000, TokensEstWithTests: 16000,
		DuplicationPct: 20, ExportedSymbols: 30, UntestedExports: 15, Globals: 2, InitFuncs: 1,
		TestFuncs: 12, HasTests: true,
	}
	before := score.Estimate(m, cfg.Rebuild).AgentPasses
	m.TokensEstWithTests += 1000
	after := score.Estimate(m, cfg.Rebuild).AgentPasses
	t.Logf("worked example agent_passes %.4f; with 1000 more test tokens %.4f", before, after)
	if after <= before {
		t.Errorf("agent_passes %v -> %v: adding test tokens no longer raises it; revisit SPEC.md 7.5", before, after)
	}
}

// isBoolMetric reports whether name is a RawMetrics field that holds a
// boolean, which setMetric sets to false for 0 and true otherwise.
func isBoolMetric(name string) bool {
	switch name {
	case "has_tests", "uses_cgo", "uses_reflect":
		return true
	}
	return false
}

// setMetric sets the metric named by its JSON field name to v, which must be
// a whole number unless the field is a float; a bool field is set to v != 0.
func setMetric(m *metrics.RawMetrics, name string, v float64) error {
	fields := map[string]any{}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if _, ok := fields[name]; !ok {
		return fmt.Errorf("unknown metric %q", name)
	}
	if isBoolMetric(name) {
		fields[name] = v != 0
	} else {
		fields[name] = v
	}
	if data, err = json.Marshal(fields); err != nil {
		return err
	}
	var out metrics.RawMetrics
	if err := json.Unmarshal(data, &out); err != nil {
		return fmt.Errorf("setting %s to %v: %w", name, v, err)
	}
	*m = out
	return nil
}

// isKnown reports whether name is a RawMetrics field, the check Config.Validate
// applies to threshold metrics.
func isKnown(name string) bool {
	return slices.Contains(metrics.MetricNames(), name)
}

// randomThresholds draws a valid rule set: each metric gets at most one rule,
// of a random kind, with a non-negative max_delta, a positive max and a
// warn_at in (0, 1).
func randomThresholds(rng *rand.Rand) []gate.Threshold {
	names := metrics.MetricNames()
	rng.Shuffle(len(names), func(i, j int) { names[i], names[j] = names[j], names[i] })
	names = names[:1+rng.IntN(len(names))]
	positive := func() *float64 {
		v := math.Round((0.1+rng.Float64()*1000)*10) / 10
		return &v
	}
	rules := make([]gate.Threshold, 0, len(names))
	for _, name := range names {
		r := gate.Threshold{Metric: name}
		switch rng.IntN(3) {
		case 0:
			r.Kind = gate.Density
			d := float64(rng.IntN(3)) * rng.Float64() * 5
			r.MaxDelta = &d
			if rng.IntN(2) == 0 {
				r.Max = positive()
			}
			r.RatchetFromZero = rng.IntN(2) == 0
		case 1:
			r.Kind = gate.Capacity
			r.Max = positive()
			r.WarnAt = 0.01 + rng.Float64()*0.98
		default:
			r.Kind = gate.Requirement
			req := rng.IntN(2) == 0
			r.Require = &req
		}
		if rng.IntN(3) == 0 {
			all := metrics.MetricNames()
			r.When = &gate.Condition{Metric: all[rng.IntN(len(all))], Value: float64(rng.IntN(1000))}
		}
		rules = append(rules, r)
	}
	return rules
}

// violations formats a result's violations for an error message.
func violations(res gate.Result) string {
	parts := make([]string, 0, len(res.Violations))
	for _, v := range res.Violations {
		parts = append(parts, fmt.Sprintf("%s base %v head %v limit %s", v.Metric, v.Base, v.Head, v.Limit))
	}
	return strings.Join(parts, "; ")
}

// identicalTrees evaluates n random records against themselves as baseline
// and returns an error for the first that violates rules. Per SPEC.md 8.1 no
// rule may fire: density deltas are 0 within a non-negative max_delta, no
// density or capacity max was introduced (a ceiling the baseline already
// breached is a warning), and requirements apply only when sloc grew.
func identicalTrees(rng *rand.Rand, rules []gate.Threshold, n int) error {
	for i := range n {
		head := randomMetrics(rng)
		base := head
		res := gate.Evaluate(head, &base, rules, nil)
		if len(res.Violations) > 0 {
			return fmt.Errorf("draw %d: identical head and baseline violate: %s", i, violations(res))
		}
	}
	return nil
}

// nonNegativeDeltas returns rules without the density rules whose max_delta
// is negative: those demand improvement, so an unchanged tree rightly fails
// them and they are outside the invariant.
func nonNegativeDeltas(t *testing.T, rules []gate.Threshold) []gate.Threshold {
	t.Helper()
	return slices.DeleteFunc(slices.Clone(rules), func(r gate.Threshold) bool {
		if r.MaxDelta != nil && *r.MaxDelta < 0 {
			t.Logf("excluding %s: max_delta %v demands improvement", r.Metric, *r.MaxDelta)
			return true
		}
		return false
	})
}

func TestIdenticalTreesNeverViolate(t *testing.T) {
	cfg := loadConfig(t)
	t.Logf("seed %d", seed)
	t.Run("config thresholds", func(t *testing.T) {
		rng := rand.New(rand.NewPCG(seed, seed))
		if err := identicalTrees(rng, nonNegativeDeltas(t, cfg.Thresholds), gateIterations); err != nil {
			t.Error(err)
		}
	})
	t.Run("randomized thresholds", func(t *testing.T) {
		rng := rand.New(rand.NewPCG(seed, seed+1))
		for set := range thresholdSets {
			rules := randomThresholds(rng)
			if err := validRules(rules); err != nil {
				t.Fatalf("set %d: generator drew an invalid rule: %v", set, err)
			}
			if err := identicalTrees(rng, rules, drawsPerThresholdSet); err != nil {
				t.Fatalf("set %d (%d rules): %v", set, len(rules), err)
			}
		}
	})
	// The property must be able to fail: a negative max_delta demands
	// improvement, so an unchanged tree breaks it.
	t.Run("negative max_delta is caught", func(t *testing.T) {
		d := -1.0
		rules := []gate.Threshold{{Metric: "func_count", Kind: gate.Density, MaxDelta: &d}}
		if err := identicalTrees(rand.New(rand.NewPCG(seed, seed)), rules, 1); err == nil {
			t.Error("identicalTrees passed a rule demanding improvement, want a violation")
		}
	})
}

// validRules checks every rule with Threshold.Validate, as the config loader
// would.
func validRules(rules []gate.Threshold) error {
	errs := make([]error, 0, len(rules))
	for _, r := range rules {
		errs = append(errs, r.Validate(isKnown))
	}
	return errors.Join(errs...)
}

// bigFeature is the "more features" half of SPEC.md 8.1: from a random
// baseline, every capacity metric grows to a random value no larger than its
// max while every density metric stays unchanged, and the result must not
// violate rules. Requirement rules are held satisfied at head, since a
// requirement fires when sloc grew (a real feature adds tests); for the
// default config that means has_tests stays true. A metric named by a density
// rule is never changed, and one named by a requirement rule is changed only
// to satisfy it.
func bigFeature(rng *rand.Rand, rules []gate.Threshold, n int) error {
	density := map[string]bool{}
	required := map[string]bool{}
	for _, r := range rules {
		switch r.Kind {
		case gate.Density:
			density[r.Metric] = true
		case gate.Requirement:
			required[r.Metric] = true
		}
	}
	for i := range n {
		base := randomMetrics(rng)
		head := base
		for _, r := range rules {
			switch {
			case r.Kind == gate.Capacity && !density[r.Metric] && !required[r.Metric]:
				top := math.Ceil(*r.Max) - 1
				if isBoolMetric(r.Metric) {
					top = math.Min(top, 1)
				}
				top = math.Max(top, 0)
				b := float64(rng.IntN(int(top) + 1))
				h := b + float64(rng.IntN(int(top-b)+1))
				if err := setMetric(&base, r.Metric, b); err != nil {
					return err
				}
				if err := setMetric(&head, r.Metric, h); err != nil {
					return err
				}
			case r.Kind == gate.Requirement && !density[r.Metric] && r.Require != nil:
				if h, ok := head.Value(r.Metric); ok && (h != 0) == *r.Require {
					continue
				}
				v := 0.0
				if *r.Require {
					v = 1
				}
				if err := setMetric(&head, r.Metric, v); err != nil {
					return err
				}
			}
		}
		if res := gate.Evaluate(head, &base, rules, nil); !res.Passed {
			return fmt.Errorf("draw %d: capacity growth below max violates: %s", i, violations(res))
		}
	}
	return nil
}

func TestCapacityGrowthBelowMaxNeverViolates(t *testing.T) {
	cfg := loadConfig(t)
	t.Logf("seed %d", seed)
	t.Run("config thresholds", func(t *testing.T) {
		rng := rand.New(rand.NewPCG(seed, seed+2))
		if err := bigFeature(rng, nonNegativeDeltas(t, cfg.Thresholds), gateIterations); err != nil {
			t.Error(err)
		}
	})
	t.Run("randomized thresholds", func(t *testing.T) {
		rng := rand.New(rand.NewPCG(seed, seed+3))
		for set := range thresholdSets {
			rules := randomThresholds(rng)
			if err := bigFeature(rng, rules, drawsPerThresholdSet); err != nil {
				t.Fatalf("set %d (%d rules): %v", set, len(rules), err)
			}
		}
	})
}
