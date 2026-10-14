package report

import (
	"fmt"
	"strings"

	"github.com/rfizzle/astimate/calibration/validate/internal/measure"
)

// candidateText introduces the candidate rules section; its verbs are the
// configuration they came from and the basis view.
const candidateText = "A candidate rule is a top-level rule of `%s` on a metric the configuration does not gate. The replays never gated it, so it is re-evaluated on every row it would judge, at its own setting, as a sweep re-evaluates a rule (Method). Each table holds every view: the gate as replayed, without the candidate, then the gate with the candidate added alone, and the change in J (recall minus false-failure rate). The candidate's own rates are in Per rule, its sweep, every setting with the gate's rates at it, in Sweeps on the %s view, and its recommendation, whether to add it, in Recommendations; Advised never adds a candidate.\n\n"

// candidates writes, per candidate rule, every view's gate without and
// with it, under both label sets when there are two.
func candidates(b *strings.Builder, in *Input) {
	idx := candidateIndexes(in.Rules)
	if len(idx) == 0 {
		return
	}
	fmt.Fprintf(b, "## Candidate rules\n\n"+candidateText, in.CandidateSource, in.basis().Name)
	head := []string{"View", "Labels", "Gate recall", "Gate false failures", "Gate J", "With recall", "With false failures", "With J", "J change"}
	for _, i := range idx {
		fmt.Fprintf(b, "### %s, %s\n\n", code(ruleName(in.Rules, i)), settingsText(in.Rules[i]))
		rows := make([][]string, 0, 2*len(in.Views))
		for vi := range in.Views {
			rows = append(rows, candidateRow(&in.Views[vi], i, "first"))
			if len(in.Second) > 0 {
				rows = append(rows, candidateRow(&in.Second[vi], i, "second"))
			}
		}
		table(b, head, rows)
	}
}

// candidateIndexes returns the indexes of rules' candidates, in order.
func candidateIndexes(rules []measure.Rule) []int {
	var out []int
	for i, r := range rules {
		if r.Candidate {
			out = append(out, i)
		}
	}
	return out
}

// settingsText states r's limits as the sweeps name them, "max_delta 0,
// floor 10", or "no limit".
func settingsText(r measure.Rule) string {
	params := measure.Params(r.Threshold)
	if len(params) == 0 {
		return "no limit"
	}
	parts := make([]string, len(params))
	for j, p := range params {
		parts[j] = measure.Shipped(r.Threshold, p).String()
	}
	return strings.Join(parts, ", ")
}

// candidateRow is one row of a candidate's table: v's gate without and
// with rule i, under the label set named labels.
func candidateRow(v *View, i int, labels string) []string {
	g, w := v.Gate, v.WithCandidate[i]
	return []string{v.Name, labels,
		frac(g.BlockFailed, g.Block), frac(g.AllowFailed, g.Allow), pts(g.J()),
		frac(w.BlockFailed, w.Block), frac(w.AllowFailed, w.Allow), pts(w.J()),
		pts(w.J() - g.J())}
}
