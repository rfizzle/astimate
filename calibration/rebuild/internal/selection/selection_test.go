package selection

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// candidate returns a candidate of module with the given tier, tests and
// rebuild_tokens.
func candidate(module, pkg string, tier score.Tier, tested bool, tokens float64) Candidate {
	return Candidate{
		Row:      Row{Module: module, Package: module + "/" + pkg, Metrics: metrics.RawMetrics{HasTests: tested}},
		Tier:     tier,
		Estimate: score.Rebuild{RebuildTokens: tokens},
	}
}

func TestSelectSpreadsAndReplaces(t *testing.T) {
	var cands []Candidate
	for i := range 10 {
		mod := fmt.Sprintf("m%d", i%5)
		cands = append(cands,
			candidate(mod, fmt.Sprintf("one%d", i), score.TierOnePass, true, float64(100*(10-i))),
			candidate(mod, fmt.Sprintf("few%d", i), score.TierFewPasses, false, float64(30000+i)))
	}
	reject := "m4/one9" // the smallest ONE_PASS tested candidate
	check := func(_ context.Context, c Candidate) (definition.Experiment, error) {
		if c.Row.Package == reject {
			return definition.Experiment{}, errors.New("oracle tests fail before stubbing")
		}
		return definition.Experiment{Package: c.Row.Package}, nil
	}
	run := func() []Verdict {
		v, err := Select(context.Background(), slices.Clone(cands), 2, 3, check)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	got := run()
	var taken []string
	for _, v := range got {
		if v.OK {
			taken = append(taken, v.Stratum+" "+v.Candidate.Row.Package)
		}
	}
	// ONE_PASS tested sorted by tokens: one9 (rejected), one8, ..., one0.
	// Slot 0 starts at index 0 and falls through to one8; slot 1 starts at
	// index 5, one4. FEW_PASSES untested: few0 and few5.
	want := []string{
		"ONE_PASS tested m3/one8", "ONE_PASS tested m4/one4",
		"FEW_PASSES untested m0/few0", "FEW_PASSES untested m0/few5",
	}
	if !slices.Equal(taken, want) {
		t.Fatalf("taken = %v, want %v", taken, want)
	}
	if got[0].OK || got[0].Reason == "" {
		t.Fatalf("first verdict = %+v, want the rejection of %s", got[0], reject)
	}
	again := run()
	if len(again) != len(got) {
		t.Fatal("Select is not deterministic")
	}
	for i := range got {
		if got[i].Candidate.Row.Package != again[i].Candidate.Row.Package || got[i].OK != again[i].OK {
			t.Fatal("Select is not deterministic")
		}
	}
}

func TestSelectModuleCap(t *testing.T) {
	var cands []Candidate
	for i := range 4 {
		cands = append(cands, candidate("m", fmt.Sprintf("p%d", i), score.TierOnePass, true, float64(i)))
	}
	cands = append(cands, candidate("n", "q", score.TierOnePass, true, 10))
	accept := func(_ context.Context, c Candidate) (definition.Experiment, error) {
		return definition.Experiment{Package: c.Row.Package}, nil
	}
	got, err := Select(context.Background(), cands, 3, 1, accept)
	if err != nil {
		t.Fatal(err)
	}
	var taken []string
	for _, v := range got {
		taken = append(taken, v.Candidate.Row.Package)
	}
	if want := []string{"m/p0", "n/q"}; !slices.Equal(taken, want) {
		t.Fatalf("taken = %v, want %v", taken, want)
	}
}

func TestCandidatesFilters(t *testing.T) {
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	base := metrics.RawMetrics{Files: 1, SLOC: 100, LargestFileSLOC: 100, TokensEst: 1000, TokensEstWithTests: 2000,
		FuncCount: 3, TestFuncs: 2, HasTests: true, TestFiles: 1, UsesCgo: new(false), GeneratedFiles: new(0)}
	row := func(pkg string, edit func(m *metrics.RawMetrics)) Row {
		m := base
		edit(&m)
		est := score.Estimate(m, cfg.Rebuild)
		return Row{Module: "example.com/m", Commit: strings.Repeat("a", 40), Package: "example.com/m/" + pkg,
			Metrics: m, AgentPasses: est.AgentPassesRounded()}
	}
	rows := []Row{
		row("keep", func(*metrics.RawMetrics) {}),
		row("cgo", func(m *metrics.RawMetrics) { m.UsesCgo = new(true) }),
		row("generated", func(m *metrics.RawMetrics) { m.GeneratedFiles = new(1) }),
		row("nofuncs", func(m *metrics.RawMetrics) { m.FuncCount = 0 }),
		row("big", func(m *metrics.RawMetrics) { m.TokensEst, m.TokensEstWithTests = 900000, 900000 }),
		row("orphan", func(m *metrics.RawMetrics) { m.HasTests, m.TestFuncs = false, 0 }),
		row("imported", func(m *metrics.RawMetrics) { m.HasTests, m.TestFuncs, m.FanIn = false, 0, 2 }),
		{Module: "std", Package: "errors", Metrics: base},
		{Module: "example.com/other", Package: "example.com/other", Metrics: base},
	}
	repos := map[string]string{"example.com/m": "https://example.com/m.git", "std": ""}
	got, err := Candidates(rows, repos, cfg.Rebuild, 10)
	if err != nil {
		t.Fatal(err)
	}
	var pkgs []string
	for _, c := range got {
		pkgs = append(pkgs, c.Row.Package)
		if c.Repo != "https://example.com/m.git" || c.Tier != score.TierOnePass {
			t.Errorf("%s: repo %q tier %s", c.Row.Package, c.Repo, c.Tier)
		}
	}
	if want := []string{"example.com/m/keep", "example.com/m/imported"}; !slices.Equal(pkgs, want) {
		t.Fatalf("candidates = %v, want %v", pkgs, want)
	}

	stale := rows[:1]
	stale[0].AgentPasses++
	if _, err := Candidates(stale, repos, cfg.Rebuild, 10); err == nil || !strings.Contains(err.Error(), "recomputes") {
		t.Fatalf("Candidates with a stale estimate = %v, want a recompute error", err)
	}
}

// TestReadRows checks that ReadRows parses one JSON object per line and
// reports a missing file.
func TestReadRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "packages.jsonl")
	data := `{"module":"example.com/m","commit":"a","package":"example.com/m/p","agent_passes":0.2,"human_days":1}` + "\n" +
		`{"module":"example.com/m","commit":"a","package":"example.com/m/q","agent_passes":1.5,"human_days":2}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadRows(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Package != "example.com/m/p" || rows[1].AgentPasses != 1.5 {
		t.Fatalf("ReadRows() = %+v", rows)
	}
	if _, err := ReadRows(filepath.Join(t.TempDir(), "missing.jsonl")); err == nil {
		t.Fatal("ReadRows of a missing file succeeded")
	}
}

// TestNewExperiment checks that NewExperiment fills an experiment from a
// candidate, the pin check's oracle and stub hash.
func TestNewExperiment(t *testing.T) {
	c := Candidate{
		Row: Row{Module: "example.com/m", Commit: strings.Repeat("a", 40), Package: "example.com/m/pkg",
			Metrics: metrics.RawMetrics{HasTests: true}, AgentPasses: 0.4, HumanDays: 1.2},
		Repo:     "https://example.com/m.git",
		Estimate: score.Rebuild{RebuildTokens: 999.6},
		Tier:     score.TierOnePass,
	}
	oracle := definition.Oracle{Test: []string{"./pkg"}, Build: []string{"./..."}}
	e := NewExperiment(c, oracle, strings.Repeat("b", 64), 100)
	if e.Module != c.Row.Module || e.Repo != c.Repo || e.Commit != c.Row.Commit || e.Package != c.Row.Package {
		t.Fatalf("NewExperiment identity fields = %+v", e)
	}
	if e.Dir != "pkg" || e.Stub != definition.StubSignatures || e.StubSHA256 != strings.Repeat("b", 64) {
		t.Fatalf("NewExperiment stub fields = %+v", e)
	}
	if !slices.Equal(e.Oracle.Test, oracle.Test) || !slices.Equal(e.Oracle.Build, oracle.Build) ||
		e.TurnCap != 100 || !e.HasTests || e.Tier != score.TierOnePass {
		t.Fatalf("NewExperiment check fields = %+v", e)
	}
	if e.AgentPasses != 0.4 || e.RebuildTokens != 1000 || e.HumanDays != 1.2 {
		t.Fatalf("NewExperiment estimate fields = %+v", e)
	}
}
