package report

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/calibration/validate/internal/measure"
)

// methodText is the Method section; its verbs are, in order, what happens
// to non-agent labels, the recall and false-failure targets, the recheck's
// mismatches and pairs checked, the false-failure target again, the basis
// view, and the rule-labeled corpora's scored block labels and how many of
// them cite a revert.
const methodText = "## Method\n\n" +
	"- Replay rows join labels on the full commit hash. A commit the replay did not load has no verdict and is left out; %s.\n" +
	"- The gate fails a commit when any of its package rows or its module row has a violation, as the replay recorded it. Recall is the share of `block` commits failed; the false-failure rate is the share of `allow` commits failed. The targets (SPEC.md 14) are recall at least %s and false failures at most %s.\n" +
	"- A rule fired on a commit when one of the commit's rows has a recorded violation of it. Its precision is the share of the commits it fired on that are `block`; its recall the share of `block` commits it fired on; its false-failure share the share of `allow` commits it fired on. A language override's rule is its own row and judges only that language's rows.\n" +
	"- A sweep re-evaluates one rule at another limit with `gate.Evaluate` over each row's stored head and baseline metrics, the rule alone and only on the rows it judges (the module row for `dup_blocks_cross_pkg`, package rows otherwise), so a capacity or density `max` fires only for a new row or a rising value, and `max_delta` compares head with base and applies to a new row only under `ratchet_from_zero`; a module row judged against an empty baseline is compared with the zero row the replay gated it against. The gate columns keep every other rule as recorded. Re-evaluating every rule at its shipped limits reproduces the recorded violations: %d of %d (row, rule) pairs disagree.\n" +
	"- The size-only rule fails a commit when one of its package rows has `sloc_delta` above t. Its threshold is the one with the highest recall whose false-failure rate is at most %s on the %s view; with none, the one with the highest J (recall minus false-failure rate).\n" +
	"- A `block` label is capacity-only when every rule it names is a capacity rule and it cites no fix-up or revert. Such a label rests on the sizes the capacity rules read, so the gate is bound to agree with it (`calibration/notes/astimate-labels-2026-09-28.md`); a corpus with any is also reported with them set aside, and that view is the honest one.\n" +
	"- The rule-labeled corpora mark a commit `block` when a later commit reverted it or a later fix changed a function it changed (`calibration/notes/agent-commits-corpus-2026-09-28.md`); of their %d scored `block` labels, %d cite a revert and the rest a fix-up. A fix-up says the change had a defect, not that it made a package harder to maintain, and an unrelated fix in the same function marks it too. Their recall measures what share of later-fixed changes the gate would have stopped, which is not what the gate claims to catch.\n\n"

// legacyText introduces the capacity table; its verb is the basis view.
const legacyText = "On the %s view: the commits each capacity rule fired on, and those where every package it fired on was already over the `max` at the parent, so the violation was growth past a ceiling passed earlier rather than a crossing. A retuned `max` does not change how often such a package grows.\n\n"

// adviceText states the recommendation criteria; its verbs are the basis
// view, MinFired, the budget and MinJ.
const adviceText = "Judged on the %s view. A rule that fired on fewer than %d commits is kept as too thin to judge. The budget is %s of `allow` commits, the whole gate's false-failure target, which no one rule may use up alone; the best setting within it is the swept setting that fails the most `block` commits while failing at most that share of `allow` commits. A rule with no setting within the budget, or whose best setting there has J (its recall minus its false-failure share) under %s, is dropped: it fails about as large a share of `allow` commits as of `block` commits. A rule over the budget as shipped is retuned to its best setting within it. Every other rule is kept: while the gate fails far more `allow` commits than its target, no rule is made stricter. Most `block` labels are fix-ups (Method), so a retune is a direction for the next calibration, not a fitted value.\n\n"

// table writes a Markdown table, escaping each cell's pipes and newlines.
func table(b *strings.Builder, head []string, rows [][]string) {
	line := func(cells []string) {
		b.WriteString("|")
		for _, c := range cells {
			c = strings.ReplaceAll(strings.ReplaceAll(c, "|", `\|`), "\n", " ")
			b.WriteString(" " + c + " |")
		}
		b.WriteString("\n")
	}
	line(head)
	b.WriteString("|" + strings.Repeat(" --- |", len(head)) + "\n")
	for _, r := range rows {
		line(r)
	}
	b.WriteString("\n")
}

// shares formats r's recall and false-failure rate.
func shares(r measure.Rates) []string {
	return []string{pct(r.Recall()), pct(r.FalseFailure())}
}

// ints formats each of vs.
func ints(vs ...int) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = strconv.Itoa(v)
	}
	return out
}

// code wraps s in backticks.
func code(s string) string { return "`" + s + "`" }

// frac formats n of d with its percentage.
func frac(n, d int) string {
	return strconv.Itoa(n) + " of " + strconv.Itoa(d) + " (" + pct(share(n, d)) + ")"
}

// share is n over d, or 0 when d is 0.
func share(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// pct formats a share as a percentage with one decimal.
func pct(v float64) string { return strconv.FormatFloat(100*v, 'f', 1, 64) + "%" }

// pts formats a difference of shares as signed percentage points.
func pts(v float64) string { return fmt.Sprintf("%+.1f pts", 100*v) }

// met is the word for whether a target is met.
func met(ok bool) string {
	if ok {
		return "met"
	}
	return "not met"
}
