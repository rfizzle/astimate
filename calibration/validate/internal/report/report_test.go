package report

import (
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/replay/labels"
	"github.com/rfizzle/astimate/calibration/validate/internal/corpus"
	"github.com/rfizzle/astimate/calibration/validate/internal/measure"
	"github.com/rfizzle/astimate/internal/gate"
)

func input() *Input {
	five := 5.0
	rules := []measure.Rule{
		{Threshold: gate.Threshold{Metric: "sloc", Kind: gate.Capacity, Max: &five}},
		{Language: "typescript", Threshold: gate.Threshold{Metric: "sloc", Kind: gate.Capacity, Max: &five}},
	}
	gateRates := measure.Rates{Block: 10, Allow: 20, BlockFailed: 9, AllowFailed: 5}
	ruleRates := measure.Rates{Block: 10, Allow: 20, BlockFailed: 8, AllowFailed: 4}
	c := &corpus.Corpus{Name: "demo", Labeled: 31, NotLoaded: 1, RuleLabels: true, Commits: []corpus.Commit{
		{Hash: "0123456789abcdef", Subject: "a | b", Label: labels.Label{Verdict: labels.Block, Reason: "fixed up by x"}},
	}}
	pt := measure.Point{Setting: measure.Setting{Param: measure.Max, Value: 5}, Shipped: true, Rule: ruleRates, Gate: gateRates}
	return &Input{
		Date: "2026-09-28", Version: "v1", ConfigSource: "the embedded default", AgentOnly: true,
		Targets: Targets{Recall: 0.8, FalseFailure: 0.1},
		Corpora: []Corpus{{Source: corpus.Source{Name: "demo", Data: "d", Labels: "l"}, Loaded: c, SetAside: 2, Checked: 7}},
		Views: []View{{Name: "demo", Gate: gateRates, Advised: ruleRates, Size: ruleRates,
			Rules: []measure.Rates{ruleRates, {}}, Judged: []int{30, 0}}},
		Rules:     rules,
		SizeT:     150,
		SizeCurve: []measure.SizePoint{{T: 0, Rates: gateRates}, {T: 150, Rates: ruleRates}},
		Curves:    [][]measure.Curve{{{Rule: 0, Param: measure.Max, Points: []measure.Point{pt}}}, nil},
		Dropped:   []measure.Rates{{Block: 10, Allow: 20, BlockFailed: 1}, {}},
		Advice: []measure.Advice{{Action: measure.Retune, To: measure.Setting{Param: measure.Max, Value: 9}, Why: "numbers"},
			{Action: measure.Keep, Why: "no row"}},
		Criteria: measure.Criteria{MinFired: 10, Budget: 0.1, MinJ: 0.02},
		Legacy:   []Legacy{{Rule: 0, Fired: measure.Rates{Block: 8, Allow: 4, BlockFailed: 8, AllowFailed: 4}, Over: measure.Rates{Block: 6, Allow: 3, BlockFailed: 6, AllowFailed: 3}}},
		Missed:   []Missed{{Corpus: "demo", Commits: []*corpus.Commit{&c.Commits[0]}}, {Corpus: "other"}},
	}
}

func TestRender(t *testing.T) {
	out := Render(input())
	for _, want := range []string{
		"# Gate validation 2026-09-28",
		"- Corpus `demo`: rows `d`, labels `l`",
		"0 of 7 (row, rule) pairs disagree",
		"of their 1 scored `block` labels, 0 cite a revert",
		"| demo | 9 of 10 (90.0%) | met | 5 of 20 (25.0%) | not met | 8 of 10 (80.0%) | 4 of 20 (20.0%) | 80.0% | 20.0% |",
		"the gate fails 90.0% of `block` commits against a target of at least 80.0% (met) and 25.0% of `allow` commits against a target of at most 10.0% (not met)",
		"the size-only rule's best J is +65.0 pts, at `sloc_delta > 0` (90.0% and 25.0%)",
		"`demo` has 2 capacity-only `block` labels",
		"| demo | rule (fix-up or revert) | 31 | 1 | 0 | 1 | 0 | 2 |",
		"| `sloc` | 8 | 4 | 66.7% | 80.0% | 20.0% |",
		"| `sloc (typescript)` | no rows |",
		"| size-only, `sloc_delta > 150` | 8 | 4 | 66.7% | 80.0% | 20.0% |",
		"No t keeps false failures at or under 10.0%; t 150 has the highest J.",
		"| max 5 * | 80.0% | 20.0% | +60.0 pts | 90.0% | 25.0% |",
		"| rule dropped |  |  |  | 10.0% | 0.0% |",
		"Nothing to sweep",
		"| `sloc` | 8 | 4 | 6 | 3 | 9 of 12 (75.0%) |",
		"| `sloc` | retune | max 9 | numbers; 75.0% of the commits it fired on only grew a package already over its max",
		"| `sloc (typescript)` | keep |  | no row |",
		"| `0123456789ab` | a \\| b | fixed up by x |",
		"### other\n\nNone.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}

func TestRenderSizeMet(t *testing.T) {
	in := input()
	in.SizeMet = true
	in.AgentOnly = false
	out := Render(in)
	if !strings.Contains(out, "Best t: 150, the highest recall with false failures at most 10.0%.") || !strings.Contains(out, "every label is scored") {
		t.Error("size met or agent text missing")
	}
}
