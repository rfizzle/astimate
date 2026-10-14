package measure

import (
	"errors"
	"testing"

	"github.com/rfizzle/astimate/calibration/validate/internal/corpus"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// linesRow returns a row of language lang whose duplicated_lines went
// from base to head, with nothing recorded, as a replay that did not gate
// the metric writes it.
func linesRow(lang string, base, head int) corpus.Row {
	return corpus.Row{Package: "p", Language: lang,
		Head: metrics.RawMetrics{SLOC: 400, HasTests: true, DuplicatedLines: &head},
		Base: &metrics.RawMetrics{SLOC: 400, HasTests: true, DuplicatedLines: &base}}
}

// TestCandidate checks a candidate rule: it is the only rule on its
// metric, judges rows of every language, is re-evaluated for its own rates
// and its sweep, stays out of the gate as replayed and of the recheck, and
// adds to the gate through With.
func TestCandidate(t *testing.T) {
	s := defaultRules(t)
	floor := 10.0
	cand := gate.Threshold{Metric: "duplicated_lines", Kind: gate.Density, MaxDelta: new(float64), Floor: &floor}
	if _, err := s.AddCandidate(gate.Threshold{Metric: "dup_blocks", Kind: gate.Density, MaxDelta: new(float64)}); !errors.Is(err, ErrGated) {
		t.Errorf("AddCandidate(dup_blocks) error = %v, want ErrGated", err)
	}
	i, err := s.AddCandidate(cand)
	if err != nil {
		t.Fatal(err)
	}
	if got := Shipped(cand, Floor).String(); got != "floor 10" {
		t.Errorf("Shipped(floor) = %q, want floor 10", got)
	}
	if got := s.Candidates(); len(got) != 1 || got[0] != i || s.List[i].Name() != "duplicated_lines (candidate)" {
		t.Fatalf("Candidates() = %v, name %q", got, s.List[i].Name())
	}
	c := &corpus.Corpus{Name: "c", Commits: []corpus.Commit{
		commit("b1", true, linesRow("go", 135, 157)),
		commit("b2", true, linesRow("typescript", 12, 18)),
		commit("a1", false, linesRow("go", 3, 9)),
		commit("a2", false, linesRow("go", 40, 41)),
		commit("a3", false, linesRow("go", 50, 50)),
	}}
	sc, err := Score(c, s)
	if err != nil {
		t.Fatal(err)
	}
	if g := sc.Gate(); g.Failed() != 0 {
		t.Errorf("Gate() = %+v, want the candidate left out", g)
	}
	if r := sc.Rule(i); r.BlockFailed != 2 || r.AllowFailed != 1 {
		t.Errorf("Rule(candidate) = %+v, want 2 block and 1 allow", r)
	}
	if w := sc.With(map[int]*gate.Threshold{i: &cand}); w.BlockFailed != 2 || w.AllowFailed != 1 {
		t.Errorf("With(candidate) = %+v, want 2 block and 1 allow", w)
	}
	if checked, mismatched := sc.Recheck(); mismatched != 0 || checked == 0 {
		t.Errorf("Recheck() = %d checked, %d mismatched; want the candidate skipped", checked, mismatched)
	}
	recorded := linesRow("go", 10, 30)
	recorded.Violated = []string{"duplicated_lines"}
	if _, err := Score(&corpus.Corpus{Name: "r", Commits: []corpus.Commit{commit("x", true, recorded)}}, s); err == nil {
		t.Error("Score accepted a recorded breach of the candidate's metric")
	}
	var floors []string
	for _, curve := range sc.Sweep(i) {
		for _, p := range curve.Points {
			if curve.Param == Floor && p.Setting.Value == 20 && !p.Setting.None {
				floors = append(floors, p.Setting.String())
				if p.Rule.BlockFailed != 1 || p.Rule.AllowFailed != 1 {
					t.Errorf("floor 20: %+v, want 1 block and 1 allow", p.Rule)
				}
			}
		}
	}
	if len(floors) != 1 {
		t.Errorf("the sweep has floor 20 %d times, want once", len(floors))
	}
}
