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
}

// Name is the rule's metric, followed by its override's language in
// parentheses for an override rule.
func (r Rule) Name() string {
	if r.Language == "" {
		return r.Threshold.Metric
	}
	return r.Threshold.Metric + " (" + r.Language + ")"
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

// For returns the List index of the rule on metric that judges row r:
// the rule of r's language on that metric, when the rule's row kind
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
		if s.List[i].Threshold.Metric == metric {
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
		eqPtr(a.MaxDelta, b.MaxDelta) && eqPtr(a.OverMaxDelta, b.OverMaxDelta) &&
		a.RatchetFromZero == b.RatchetFromZero &&
		a.WarnAt == b.WarnAt && eqPtr(a.Require, b.Require) && eqPtr(a.When, b.When)
}

// eqPtr reports whether a and b are both nil or point to equal values.
func eqPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// fires reports whether t, evaluated alone by the gate, has a violation on
// row r.
func fires(t gate.Threshold, r *corpus.Row) bool {
	return len(gate.Evaluate(r.Head, r.Base, []gate.Threshold{t}, nil).Violations) > 0
}
