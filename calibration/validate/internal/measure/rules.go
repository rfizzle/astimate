// Package measure scores the gate against labeled commits: how often the
// gate as a whole, each of its rules, and a size-only rule on sloc_delta
// fail commits labeled block and commits labeled allow, and how those
// rates move when one rule's limit is swept. A rule is re-evaluated at
// another limit by internal/gate's own Evaluate over the rows' stored head
// and baseline metrics, so the sweep applies the gate's semantics (new or
// rising for a max, head minus base for max_delta, ratchet_from_zero, the
// module row for a module-wide metric) rather than a copy of them.
package measure

import (
	"errors"
	"fmt"
	"slices"

	"github.com/rfizzle/astimate/calibration/validate/internal/corpus"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// Rule is one gated rule: a top-level threshold, or one a language
// override puts in place of it or adds.
type Rule struct {
	// Language is the override's language id; empty for a top-level rule.
	Language string
	// Threshold is the rule as the configuration ships it.
	Threshold gate.Threshold
	// Candidate marks a rule the configuration lacks, measured as if it
	// were added (Rules.AddCandidate): the replays never gated it, so it
	// fires only where it is re-evaluated, and the gate as replayed leaves
	// it out.
	Candidate bool
}

// Name is the rule's metric, followed by its override's language in
// parentheses for an override rule, by "(warn)" for a warn rule, which
// reports without failing, and by "(candidate)" for a candidate.
func (r Rule) Name() string {
	name := r.Threshold.Metric
	if r.Language != "" {
		name += " (" + r.Language + ")"
	}
	if r.Threshold.Warns() {
		name += " (warn)"
	}
	if r.Candidate {
		name += " (candidate)"
	}
	return name
}

// Rules is a configuration's gated rules and which of them judges a row.
type Rules struct {
	// List holds the top-level rules in file order, then each language
	// override's rules that differ from the top level, by language.
	List []Rule
	// top and byLang hold the List indexes of the rules a row of a
	// language without and with an override is judged by.
	top    []int
	byLang map[string][]int
}

// NewRules returns cfg's gated rules.
func NewRules(cfg *config.Config) *Rules {
	s := &Rules{byLang: make(map[string][]int)}
	for i, t := range cfg.Thresholds {
		s.List = append(s.List, Rule{Threshold: t})
		s.top = append(s.top, i)
	}
	for _, lang := range cfg.Languages() {
		eff := cfg.ForLanguage(lang).Thresholds
		idx := make([]int, 0, len(eff))
		for _, t := range eff {
			i := slices.IndexFunc(cfg.Thresholds, func(u gate.Threshold) bool { return same(t, u) })
			if i < 0 {
				i = len(s.List)
				s.List = append(s.List, Rule{Language: lang, Threshold: t})
			}
			idx = append(idx, i)
		}
		s.byLang[lang] = idx
	}
	return s
}

// ErrGated is returned by AddCandidate for a candidate on a metric a rule
// of the configuration already gates, where For would never pick it.
var ErrGated = errors.New("a rule of the configuration already gates the metric")

// AddCandidate appends t as a candidate rule (Rule.Candidate) judging the
// rows of every language, and returns its List index. Its metric must be
// one no rule of s gates, so it is the only rule on that metric.
func (s *Rules) AddCandidate(t gate.Threshold) (int, error) {
	if slices.ContainsFunc(s.List, func(r Rule) bool { return r.Threshold.Metric == t.Metric }) {
		return 0, fmt.Errorf("candidate %s: %w", t.Metric, ErrGated)
	}
	i := len(s.List)
	s.List = append(s.List, Rule{Threshold: t, Candidate: true})
	s.top = append(s.top, i)
	for lang := range s.byLang {
		s.byLang[lang] = append(s.byLang[lang], i)
	}
	return i, nil
}

// Candidates returns the List indexes of the candidate rules, in order.
func (s *Rules) Candidates() []int {
	var out []int
	for i, r := range s.List {
		if r.Candidate {
			out = append(out, i)
		}
	}
	return out
}

// For returns the List index of the rule on metric that judges row r:
// the first rule of r's language on that metric whose roles take r's
// role (gate.Threshold.Judges), when the rule's row kind
// (metrics.ModuleWide on the module row, every other metric on package
// rows) is r's. ok is false when no rule does.
func (s *Rules) For(r *corpus.Row, metric string) (i int, ok bool) {
	if metrics.ModuleWide(metric) != r.Module() {
		return 0, false
	}
	idx, found := s.byLang[r.Language]
	if !found {
		idx = s.top
	}
	for _, i := range idx {
		if t := &s.List[i].Threshold; t.Metric == metric && t.Judges(r.Head.Role) {
			return i, true
		}
	}
	return 0, false
}

// Judges reports whether rule i judges row r.
func (s *Rules) Judges(i int, r *corpus.Row) bool {
	j, ok := s.For(r, s.List[i].Threshold.Metric)
	return ok && j == i
}

// same reports whether a and b are the same rule.
func same(a, b gate.Threshold) bool {
	return a.Metric == b.Metric && a.Kind == b.Kind && eqPtr(a.Max, b.Max) &&
		eqPtr(a.MaxDelta, b.MaxDelta) && eqPtr(a.OverMaxDelta, b.OverMaxDelta) && eqPtr(a.Floor, b.Floor) &&
		a.RatchetFromZero == b.RatchetFromZero &&
		a.WarnAt == b.WarnAt && eqPtr(a.Require, b.Require) && eqPtr(a.When, b.When) &&
		a.Warns() == b.Warns()
}

// eqPtr reports whether a and b are both nil or point to equal values.
func eqPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// fires reports whether t, evaluated alone by the gate at severity fail,
// has a violation on row r: whether it breaches, whatever its severity.
// A row that records its changed functions gives them to the gate, so
// changed_func_cognitive_max is judged per function (SPEC.md 8.1); on any
// other row it is judged on the row's one stored value.
func fires(t gate.Threshold, r *corpus.Row) bool {
	t.Severity = gate.SeverityFail
	var opts []gate.Option
	if r.HasChanged() {
		opts = append(opts, gate.WithChangedFunctions(r.Changed))
	}
	return len(gate.Evaluate(r.Head, r.Base, []gate.Threshold{t}, nil, opts...).Violations) > 0
}
