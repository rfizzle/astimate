package measure

import "fmt"

// Action is what the report recommends for a rule.
type Action string

// Actions.
const (
	Keep   Action = "keep"
	Retune Action = "retune"
	Drop   Action = "drop"
)

// Criteria are the numbers a recommendation is judged by.
type Criteria struct {
	// MinFired is the fewest labeled commits a rule must fail at its
	// shipped limits for the corpus to judge it; below it the rule is kept
	// as too thin to judge.
	MinFired int
	// Budget is the largest share of allow commits one rule may fail: the
	// whole gate's false-failure target, which no single rule may use up
	// alone.
	Budget float64
	// MinJ is the J (Rates.J) a rule must reach at some setting within
	// Budget to be kept: below it the rule fails about as large a share of
	// allow commits as of block commits.
	MinJ float64
}

// Advice is a rule's recommendation and the numbers behind it.
type Advice struct {
	Action Action
	// To is the setting to retune to, for Retune.
	To Setting
	// Why states the numbers that justify the action.
	Why string
}

// Advise recommends keep, retune or drop for a rule from its shipped rates,
// the commits with a row it judges, and its sweeps. A rule that judged no
// row, or fired on fewer than MinFired commits, is kept: too thin to
// judge; so is a rule with no limit to sweep. Otherwise the settings within Budget are the swept settings that
// fail at most Budget of allow commits, and the best of them the one that
// fails the most block commits (the fewest allow commits, then the shipped
// setting, on a tie). With no setting within Budget, or a best whose J is
// below MinJ, the rule is dropped. A rule whose shipped setting exceeds
// Budget is retuned to the best; any other rule is kept, since while the
// gate fails far more allow commits than its target no rule is made
// stricter.
func Advise(shipped Rates, judged int, curves []Curve, cr Criteria) Advice {
	cur := "shipped " + describe(shipped)
	switch {
	case judged == 0:
		return Advice{Action: Keep, Why: "no row it judges in these corpora; nothing measured"}
	case shipped.Failed() < cr.MinFired:
		return Advice{Action: Keep, Why: fmt.Sprintf("fired on %d labeled commits, fewer than %d, too thin to judge; %s", shipped.Failed(), cr.MinFired, cur)}
	case len(curves) == 0:
		return Advice{Action: Keep, Why: cur + "; it has no limit to sweep"}
	}
	best, ok := bestWithin(curves, cr.Budget)
	switch {
	case !ok:
		least := leastFalse(curves)
		return Advice{Action: Drop, Why: fmt.Sprintf("%s; no swept setting fails at most %s of allow (least: %s at %s)", cur, pct(cr.Budget), pct(least.Rule.FalseFailure()), least.Setting)}
	case best.Rule.J() < cr.MinJ:
		return Advice{Action: Drop, Why: fmt.Sprintf("%s; its best setting within %s of allow, %s, %s, under J %s", cur, pct(cr.Budget), best.Setting, describe(best.Rule), signedPct(cr.MinJ))}
	case shipped.FalseFailure() > cr.Budget+eps:
		return Advice{Action: Retune, To: best.Setting, Why: fmt.Sprintf("%s, over %s of allow; %s %s", cur, pct(cr.Budget), best.Setting, describe(best.Rule))}
	}
	return Advice{Action: Keep, Why: fmt.Sprintf("%s, within %s of allow", cur, pct(cr.Budget))}
}

// describe states a rule's rates at one setting.
func describe(r Rates) string {
	return fmt.Sprintf("fails %s of block and %s of allow (J %s)", pct(r.Recall()), pct(r.FalseFailure()), signedPct(r.J()))
}

// eps absorbs rounding when a share is compared with a limit.
const eps = 1e-12

// bestWithin returns the point of curves failing at most budget of allow
// commits that fails the most block commits, the fewest allow commits and
// then the shipped point winning a tie.
func bestWithin(curves []Curve, budget float64) (Point, bool) {
	var best Point
	found := false
	for _, c := range curves {
		for _, p := range c.Points {
			if p.Rule.FalseFailure() > budget+eps {
				continue
			}
			b, a := p.Rule.BlockFailed, p.Rule.AllowFailed
			better := !found || b > best.Rule.BlockFailed ||
				(b == best.Rule.BlockFailed && (a < best.Rule.AllowFailed || (a == best.Rule.AllowFailed && p.Shipped)))
			if better {
				best, found = p, true
			}
		}
	}
	return best, found
}

// leastFalse returns the point of curves that fails the fewest allow
// commits, the first on a tie; curves must hold a point.
func leastFalse(curves []Curve) Point {
	var least Point
	found := false
	for _, c := range curves {
		for _, p := range c.Points {
			if !found || p.Rule.AllowFailed < least.Rule.AllowFailed {
				least, found = p, true
			}
		}
	}
	return least
}

// SizePoint is the size-only rule at one threshold.
type SizePoint struct {
	T     int
	Rates Rates
}

// SizeCurve evaluates the size-only rule at every threshold of ts.
func (s *Scored) SizeCurve(ts []int) []SizePoint {
	out := make([]SizePoint, len(ts))
	for i, t := range ts {
		out[i] = SizePoint{T: t, Rates: s.Size(t)}
	}
	return out
}

// BestSize picks the size-only threshold with the highest recall whose
// false-failure rate is at most maxFF, the lower false-failure rate on a
// tie; met is false when no threshold stays within maxFF, and the pick is
// then the threshold with the highest J.
func BestSize(pts []SizePoint, maxFF float64) (best SizePoint, met bool) {
	for _, p := range pts {
		if p.Rates.FalseFailure() > maxFF+eps {
			continue
		}
		if !met || p.Rates.Recall() > best.Rates.Recall() ||
			(p.Rates.Recall() == best.Rates.Recall() && p.Rates.FalseFailure() < best.Rates.FalseFailure()) {
			best, met = p, true
		}
	}
	if met {
		return best, true
	}
	for i, p := range pts {
		if i == 0 || p.Rates.J() > best.Rates.J() {
			best = p
		}
	}
	return best, false
}

// pct formats a share as a percentage with one decimal.
func pct(v float64) string { return fmt.Sprintf("%.1f%%", 100*v) }

// signedPct formats a J as signed percentage points with one decimal.
func signedPct(v float64) string { return fmt.Sprintf("%+.1f pts", 100*v) }
