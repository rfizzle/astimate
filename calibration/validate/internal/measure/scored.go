package measure

import (
	"fmt"
	"slices"

	"github.com/rfizzle/astimate/calibration/validate/internal/corpus"
	"github.com/rfizzle/astimate/internal/gate"
)

// Rates counts labeled commits and how many of each verdict something
// failed: the gate, one rule, or the size-only rule.
type Rates struct {
	// Block and Allow are the labeled commits of each verdict.
	Block, Allow int
	// BlockFailed and AllowFailed are how many of them were failed.
	BlockFailed, AllowFailed int
}

// add counts one commit.
func (r *Rates) add(block, failed bool) {
	switch {
	case block:
		r.Block++
		if failed {
			r.BlockFailed++
		}
	default:
		r.Allow++
		if failed {
			r.AllowFailed++
		}
	}
}

// Recall is the share of block commits failed; 0 without block commits.
func (r Rates) Recall() float64 { return share(r.BlockFailed, r.Block) }

// FalseFailure is the share of allow commits failed; 0 without allow
// commits.
func (r Rates) FalseFailure() float64 { return share(r.AllowFailed, r.Allow) }

// Precision is the share of failed commits labeled block; ok is false
// when nothing was failed.
func (r Rates) Precision() (p float64, ok bool) {
	n := r.BlockFailed + r.AllowFailed
	return share(r.BlockFailed, n), n > 0
}

// Failed is the number of commits failed.
func (r Rates) Failed() int { return r.BlockFailed + r.AllowFailed }

// J is Youden's index, Recall minus FalseFailure: how much larger a
// share of block commits than of allow commits is failed.
func (r Rates) J() float64 { return r.Recall() - r.FalseFailure() }

// share is n over d, or 0 when d is 0.
func share(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// Scored is a corpus with each commit's recorded violations resolved to
// the rules that fired.
type Scored struct {
	// Corpus is the corpus scored.
	Corpus *corpus.Corpus
	// Rules are the gated rules the replay ran with.
	Rules *Rules
	// fired holds, per commit, whether each rule had a recorded violation
	// on one of its rows.
	fired [][]bool
}

// Score resolves c's recorded violations to rules. A violation of a
// metric no rule of s gates on its row is an error: the rows were not
// gated with this configuration.
func Score(c *corpus.Corpus, s *Rules) (*Scored, error) {
	sc := &Scored{Corpus: c, Rules: s, fired: make([][]bool, len(c.Commits))}
	for ci := range c.Commits {
		cm := &c.Commits[ci]
		f := make([]bool, len(s.List))
		for ri := range cm.Rows {
			r := &cm.Rows[ri]
			for _, m := range r.Violated {
				i, ok := s.For(r, m)
				if !ok {
					return nil, fmt.Errorf("commit %s row %s: violation of %s, which no rule gates there", cm.Hash, r.Package, m)
				}
				f[i] = true
			}
		}
		sc.fired[ci] = f
	}
	return sc, nil
}

// Gate is how the gate as replayed did: a commit failed when any of its
// rows has a recorded violation.
func (s *Scored) Gate() Rates {
	var r Rates
	for i := range s.Corpus.Commits {
		c := &s.Corpus.Commits[i]
		r.add(c.Block(), c.Failed())
	}
	return r
}

// Rule is how rule i did as replayed: a commit counts as failed by it
// when one of its rows has a recorded violation of the rule.
func (s *Scored) Rule(i int) Rates {
	var r Rates
	for ci := range s.Corpus.Commits {
		r.add(s.Corpus.Commits[ci].Block(), s.fired[ci][i])
	}
	return r
}

// Judged is the number of commits with a row rule i judges.
func (s *Scored) Judged(i int) int {
	n := 0
	for ci := range s.Corpus.Commits {
		if s.judges(i, &s.Corpus.Commits[ci]) {
			n++
		}
	}
	return n
}

// judges reports whether rule i judges a row of c.
func (s *Scored) judges(i int, c *corpus.Commit) bool {
	for ri := range c.Rows {
		if s.Rules.Judges(i, &c.Rows[ri]) {
			return true
		}
	}
	return false
}

// Size is the size-only rule at t: a commit fails when a package row's
// sloc_delta exceeds t. The module row, which has no sloc, is not read.
func (s *Scored) Size(t int) Rates {
	var r Rates
	for ci := range s.Corpus.Commits {
		c := &s.Corpus.Commits[ci]
		failed := false
		for ri := range c.Rows {
			failed = failed || (!c.Rows[ri].Module() && c.Rows[ri].SLOCDelta > t)
		}
		r.add(c.Block(), failed)
	}
	return r
}

// Swap re-evaluates rule i as t on every row it judges, or drops it when
// t is nil, and returns the rule's rates and the gate's, the other rules
// firing as recorded.
func (s *Scored) Swap(i int, t *gate.Threshold) (rule, all Rates) {
	for ci := range s.Corpus.Commits {
		c := &s.Corpus.Commits[ci]
		rule.add(c.Block(), t != nil && s.firesOn(i, *t, c))
	}
	return rule, s.With(map[int]*gate.Threshold{i: t})
}

// With returns the gate's rates with each rule of over, keyed by its index
// in Rules.List, re-evaluated as its threshold, or dropped where that is
// nil, and every other rule firing as recorded.
func (s *Scored) With(over map[int]*gate.Threshold) Rates {
	var r Rates
	for ci := range s.Corpus.Commits {
		r.add(s.Corpus.Commits[ci].Block(), s.fails(ci, over))
	}
	return r
}

// fails reports whether commit ci fails with the rules of over replaced.
func (s *Scored) fails(ci int, over map[int]*gate.Threshold) bool {
	c := &s.Corpus.Commits[ci]
	for j, f := range s.fired[ci] {
		t, replaced := over[j]
		if (!replaced && f) || (replaced && t != nil && s.firesOn(j, *t, c)) {
			return true
		}
	}
	return false
}

// firesOn reports whether t, standing in for rule i, fires on a row of c
// that rule i judges.
func (s *Scored) firesOn(i int, t gate.Threshold, c *corpus.Commit) bool {
	for ri := range c.Rows {
		r := &c.Rows[ri]
		if s.Rules.Judges(i, r) && fires(t, r) {
			return true
		}
	}
	return false
}

// Recheck re-evaluates every rule at its shipped limits on every row it
// judges and returns the number of (row, rule) pairs checked and how many
// disagree with the violations the replay recorded.
func (s *Scored) Recheck() (checked, mismatched int) {
	for ci := range s.Corpus.Commits {
		c := &s.Corpus.Commits[ci]
		for ri := range c.Rows {
			r := &c.Rows[ri]
			for i, rule := range s.Rules.List {
				if !s.Rules.Judges(i, r) {
					continue
				}
				checked++
				recorded := false
				for _, m := range r.Violated {
					recorded = recorded || m == rule.Threshold.Metric
				}
				if recorded != fires(rule.Threshold, r) {
					mismatched++
				}
			}
		}
	}
	return checked, mismatched
}

// Missed returns the block commits the gate passed, in corpus order.
func (s *Scored) Missed() []*corpus.Commit {
	var out []*corpus.Commit
	for i := range s.Corpus.Commits {
		if c := &s.Corpus.Commits[i]; c.Block() && !c.Failed() {
			out = append(out, c)
		}
	}
	return out
}

// Legacy counts the commits rule i fired on as recorded and, among them,
// the commits where every row it fired on was already over the rule's max
// at its first parent: growth in a package the ceiling had already been
// passed in, not a crossing. Both are zero for a rule without a max.
func (s *Scored) Legacy(i int) (fired, over Rates) {
	t := s.Rules.List[i].Threshold
	if t.Max == nil {
		return fired, over
	}
	for ci := range s.Corpus.Commits {
		if !s.fired[ci][i] {
			continue
		}
		c := &s.Corpus.Commits[ci]
		fired.add(c.Block(), true)
		all := true
		for ri := range c.Rows {
			r := &c.Rows[ri]
			if !s.Rules.Judges(i, r) || !slices.Contains(r.Violated, t.Metric) {
				continue
			}
			b, ok := 0.0, r.Base != nil
			if ok {
				b, ok = r.Base.Value(t.Metric)
			}
			all = all && ok && b > *t.Max
		}
		if all {
			over.add(c.Block(), true)
		}
	}
	return fired, over
}
