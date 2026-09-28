// Package synth generates rebuild runs in the runner's row schema whose
// measured tokens follow the fit's model with planted parameters, so the
// fit can be checked to recover them before any real run exists.
package synth

import (
	"math"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/model"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/agent"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/runner"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// Plant describes the synthetic experiment.
type Plant struct {
	// Params are the planted parameters.
	Params model.Params
	// Noise is the standard deviation of the multiplicative noise on each
	// run's tokens, as a fraction (0.05 for 5%).
	Noise float64
	// Runs is the runs per package.
	Runs int
	// FailEvery makes every FailEvery-th run (counting over all rows) fail
	// its tests; 0 means none fails. A failing run spends its planted cost
	// and every other one of them also hits the turn cap.
	FailEvery int
	// Agent and Model name the agent on every row.
	Agent, Model string
	// Seed fixes the noise.
	Seed uint64
}

// Rows returns p.Runs rows per package of pkgs, run-major as the runner
// writes them. Each row's footprint (input + cache writes + output) is the
// planted model's prediction times (1 + p.Noise * a standard normal draw);
// cache reads are ten times the footprint, turns and wall time grow with it.
func Rows(pkgs []metrics.RawMetrics, p *Plant) []runner.RunRow {
	rng := rand.New(rand.NewPCG(p.Seed, p.Seed^0x9e3779b97f4a7c15))
	tiers := score.Tiers{OnePassMax: 1, FewPassesMax: 3}
	rows := make([]runner.RunRow, 0, len(pkgs)*p.Runs)
	start := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	n := 0
	for run := 1; run <= p.Runs; run++ {
		for i := range pkgs {
			m := pkgs[i]
			in := model.InputsOf(&m)
			want := p.Params.Measured(in)
			tokens := math.Max(want*(1+p.Noise*rng.NormFloat64()), 1)
			n++
			failed := p.FailEvery > 0 && n%p.FailEvery == 0
			capHit := failed && (n/p.FailEvery)%2 == 0
			pkg := "example.com/synth/p" + strconv.Itoa(i)
			rows = append(rows, row(pkg, run, &m, tokens, failed, capHit, p, tiers, start))
		}
	}
	return rows
}

// row builds one run's row.
func row(pkg string, run int, m *metrics.RawMetrics, tokens float64, failed, capHit bool,
	p *Plant, tiers score.Tiers, start time.Time,
) runner.RunRow {
	in := model.InputsOf(m)
	passes := p.Params.Passes(in)
	total := int64(tokens)
	input, output := total/50, total/5
	write := total - input - output
	read := 10 * total
	turns := 5 + int(total/4000)
	wallMS := 30000 + total/10
	subtype := "success"
	if capHit {
		turns = 100
		subtype = "error_max_turns"
	}
	return runner.RunRow{
		Schema: 1, Module: "example.com/synth", Package: pkg, Dir: pkg[len("example.com/synth/"):],
		Commit: "0123456789abcdef0123456789abcdef01234567", StubSHA256: "00", Run: run, TurnCap: 100,
		Agent: runner.AgentRun{Name: p.Agent, Template: "synthetic", Command: "synthetic", Model: p.Model,
			WallMS: wallMS},
		Estimate: runner.Estimate{Tier: score.TierOf(passes, tiers), AgentPasses: math.Round(passes*10) / 10,
			RebuildTokens: int(p.Params.RebuildTokens(in)), HasTests: m.TestFuncs > 0},
		Metrics:       *m,
		ConfigVersion: "synthetic",
		GoVersion:     "go1.27.1",
		Measured: agent.Measured{
			Usage: agent.Usage{InputTokens: new(input), OutputTokens: new(output), CacheReadTokens: new(read),
				CacheWriteTokens: new(write), TokenSource: new("modelUsage"), CostUSD: new(float64(total) / 1e5)},
			Turns: new(turns), ToolCalls: new(turns), DurationMS: new(wallMS), APIDurationMS: new(wallMS / 2),
			SessionID: new(pkg + "#" + strconv.Itoa(run)), ResultSubtype: new(subtype), IsError: new(capHit),
			TurnCapHit: new(capHit), Models: []string{p.Model}, Missing: []string{},
		},
		Oracle: runner.OracleOutcome{Test: []string{"./" + pkg}, Build: []string{"./..."},
			TestsPass: !failed, BuildPasses: true, Passed: !failed, Completed: true},
		Changes:    runner.Changes{TestFiles: []string{}, OutsidePackage: []string{}},
		Valid:      true,
		StartedAt:  start,
		FinishedAt: start.Add(time.Duration(wallMS) * time.Millisecond),
	}
}

// RandomMetrics returns n packages' metrics spread over the sizes the
// rebuild experiments cover (tokens_est from about 200 to 60,000, log
// uniform), with tests, exports, untested exports, hidden state,
// duplication and complexity drawn independently enough that every input
// varies on its own.
func RandomMetrics(n int, seed uint64) []metrics.RawMetrics {
	rng := rand.New(rand.NewPCG(seed, seed+1))
	out := make([]metrics.RawMetrics, n)
	for i := range out {
		tokens := int(200 * math.Exp(rng.Float64()*math.Log(300)))
		exports := 1 + rng.IntN(80)
		funcs := exports + rng.IntN(60)
		hasTests := rng.IntN(6) != 0
		testTokens, testFuncs := 0, 0
		if hasTests {
			testTokens = int(float64(tokens) * 1.5 * rng.Float64())
			testFuncs = 1 + rng.IntN(40)
		}
		out[i] = metrics.RawMetrics{
			Files: 1 + tokens/3000, SLOC: tokens / 8, LargestFileSLOC: tokens / 16,
			TokensEst: tokens, TokensEstWithTests: tokens + testTokens,
			ExportedSymbols: exports, UntestedExports: rng.IntN(exports + 1),
			Globals: rng.IntN(6), InitFuncs: rng.IntN(2),
			DuplicationPct: math.Round(rng.Float64()*200) / 10,
			TestFuncs:      testFuncs, TestFiles: min(testFuncs, 1+testFuncs/10), HasTests: hasTests,
			FuncCount: funcs, CognitiveTotal: funcs * (1 + rng.IntN(8)), CognitiveP90: 1 + rng.IntN(25),
			MaxNesting: 1 + rng.IntN(6), FanIn: rng.IntN(12),
			UsesCgo: new(false), GeneratedFiles: new(0),
		}
	}
	return out
}
