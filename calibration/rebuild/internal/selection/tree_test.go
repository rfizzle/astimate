package selection

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// TestTreeCandidates builds trees from the rows of one module and checks
// the members, the bounds, the cgo, generated-file and main-package
// rules, and the summed estimate.
func TestTreeCandidates(t *testing.T) {
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Rebuild
	base := metrics.RawMetrics{Files: 1, SLOC: 100, LargestFileSLOC: 100, TokensEst: 9000, TokensEstWithTests: 12000,
		FuncCount: 3, TestFuncs: 2, HasTests: true, TestFiles: 1, UsesCgo: new(false), GeneratedFiles: new(0)}
	row := func(pkg string, edit func(m *metrics.RawMetrics)) Row {
		m := base
		edit(&m)
		est := score.Estimate(m, p)
		return Row{Module: "example.com/m", Commit: strings.Repeat("a", 40), Package: "example.com/m/" + pkg,
			Metrics: m, AgentPasses: est.AgentPassesRounded(), HumanDays: 1.25}
	}
	same := func(*metrics.RawMetrics) {}
	rows := []Row{
		{Module: "example.com/m", Commit: strings.Repeat("a", 40), Package: "example.com/m", Metrics: base,
			AgentPasses: score.Estimate(base, p).AgentPassesRounded()},
		row("a", same), row("a/b", same), row("a/b/c", same), row("a/cmd", same),
		row("solo", same),
		row("g", same), row("g/gen", func(m *metrics.RawMetrics) { m.GeneratedFiles = new(1) }),
		row("x", func(m *metrics.RawMetrics) {
			m.HasTests, m.TestFuncs, m.TestFiles, m.TokensEstWithTests = false, 0, 0, 9000
		}),
		row("x/y", func(m *metrics.RawMetrics) {
			m.HasTests, m.TestFuncs, m.TestFiles, m.TokensEstWithTests = false, 0, 0, 9000
		}),
	}
	repos := map[string]string{"example.com/m": "https://example.com/m.git"}
	mains := map[string]bool{"example.com/m/a/cmd": true}
	rule := definition.SelectionRule{MinPackages: 2, MaxPackages: 3, MaxAgentPasses: 12}
	got, err := TreeCandidates(rows, repos, mains, p, rule)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range got {
		names = append(names, c.Row.Package)
	}
	// a has a, a/b, a/b/c (a/cmd is main); a/b has two; solo has one; g has
	// a generated member; x has two untested members.
	if want := []string{"example.com/m/a", "example.com/m/a/b", "example.com/m/x"}; !slices.Equal(names, want) {
		t.Fatalf("trees = %v, want %v", names, want)
	}
	a := got[0]
	var dirs []string
	tokens := 0
	for _, m := range a.Members {
		dirs = append(dirs, definition.ModRelDir(m.Row.Module, m.Row.Package))
		tokens += int(math.Round(m.Estimate.RebuildTokens))
	}
	if want := []string{"a", "a/b", "a/b/c"}; !slices.Equal(dirs, want) {
		t.Fatalf("members of a = %v, want %v", dirs, want)
	}
	want := SumEstimate(tokens, p)
	if a.Estimate.RebuildTokens != float64(tokens) || a.Estimate.AgentPasses != want.AgentPasses ||
		a.Row.AgentPasses != want.AgentPassesRounded() || a.Tier != score.TierOf(want.AgentPasses, p.Tiers) {
		t.Fatalf("tree estimate %+v tier %s, want the sum %d through the curve (%v)", a.Estimate, a.Tier, tokens, want.AgentPasses)
	}
	if a.Row.HumanDays != 3.8 || !a.Row.Metrics.HasTests || a.Row.Metrics.TokensEst != 3*base.TokensEst {
		t.Fatalf("tree row %+v", a.Row)
	}
	if got[2].Row.Metrics.HasTests {
		t.Fatal("an all-untested tree reads as tested")
	}

	e := NewExperiment(a, definition.Oracle{Test: []string{"./a/..."}, Build: []string{"./..."}}, strings.Repeat("b", 64), 100)
	if len(e.Members) != 3 || e.Members[0].Dir != "a" || e.RebuildTokens != tokens {
		t.Fatalf("tree experiment %+v", e)
	}
	if e.Members[1].RebuildTokens != int(math.Round(a.Members[1].Estimate.RebuildTokens)) || !e.Members[1].HasTests {
		t.Fatalf("member %+v", e.Members[1])
	}

	rule.MaxAgentPasses = 0.1
	if got, err := TreeCandidates(rows, repos, mains, p, rule); err != nil || len(got) != 0 {
		t.Fatalf("TreeCandidates over the passes cap = %v, %v", got, err)
	}
}

// TestSumEstimate checks the curve: linear up to the budget, the exponent
// past it.
func TestSumEstimate(t *testing.T) {
	p := score.RebuildParams{ContextBudget: 1000, SuperlinearExponent: 2}
	if got := SumEstimate(500, p); got.AgentPasses != 0.5 || got.RebuildTokens != 500 {
		t.Fatalf("SumEstimate(500) = %+v", got)
	}
	if got := SumEstimate(3000, p); got.AgentPasses != 9 || got.Ratio != 3 {
		t.Fatalf("SumEstimate(3000) = %+v", got)
	}
}

// TestMemberDefs checks that a member carries its candidate's own
// estimate, rounded as an experiment's.
func TestMemberDefs(t *testing.T) {
	c := Candidate{
		Row: Row{Module: "example.com/m", Package: "example.com/m/a/b", AgentPasses: 0.3, HumanDays: 1.5,
			Metrics: metrics.RawMetrics{HasTests: true, SLOC: 7}},
		Estimate: score.Rebuild{RebuildTokens: 1234.5}, Tier: score.TierOnePass,
	}
	got := MemberDefs([]Candidate{c})
	want := definition.Member{Package: "example.com/m/a/b", Dir: "a/b", HasTests: true, Tier: score.TierOnePass,
		AgentPasses: 0.3, RebuildTokens: 1235, HumanDays: 1.5, Metrics: definition.Metrics{RawMetrics: c.Row.Metrics}}
	if len(got) != 1 || got[0].Dir != want.Dir || got[0].RebuildTokens != want.RebuildTokens ||
		got[0].Metrics.SLOC != 7 || got[0].AgentPasses != want.AgentPasses || !got[0].HasTests {
		t.Fatalf("MemberDefs() = %+v, want %+v", got, want)
	}
}
