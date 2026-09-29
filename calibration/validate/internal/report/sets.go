package report

import (
	"fmt"
	"strings"
)

// splitRulePhrase is part of the split-or-extraction rule's text
// (calibration/replay/label), by which a labels file made by that rule is
// told from one made by the revert-and-fix-up rule.
const splitRulePhrase = "extracts duplicate code"

// labelKind names the kind of a labels file: hand labels, or the rule
// that made them.
func labelKind(ruleLabels bool, rule string) string {
	switch {
	case !ruleLabels:
		return "hand"
	case strings.Contains(rule, splitRulePhrase):
		return "rule (split or extraction)"
	}
	return "rule (fix-up or revert)"
}

// labelSets writes every view under both label sets, when the corpora
// name a second one.
func labelSets(b *strings.Builder, in *Input) {
	if len(in.Second) == 0 {
		return
	}
	t := in.Targets
	fmt.Fprintf(b, "## Label sets\n\n"+labelSetsText, in.SizeT)
	rows := make([][]string, 0, len(in.Corpora))
	for _, c := range in.Corpora {
		row := []string{c.Source.Name, labelKind(c.Loaded.RuleLabels, c.Loaded.Rule), labelKind(c.SecondRule != "", c.SecondRule)}
		for _, v := range in.Second {
			if v.Name == c.Source.Name {
				row = append(row, ints(v.Gate.Block, v.Gate.Allow)...)
			}
		}
		rows = append(rows, row)
	}
	table(b, []string{"Corpus", "First labels", "Second labels", "`block` scored, second", "`allow` scored, second"}, rows)
	rows = make([][]string, 0, 2*len(in.Views))
	for i := range in.Views {
		name := in.Views[i].Name
		rows = append(rows, append([]string{name, "first"}, targetCells(&in.Views[i], t)...),
			append([]string{name, "second"}, targetCells(&in.Second[i], t)...))
	}
	table(b, append([]string{"View", "Labels"}, targetHead(t)...), rows)
}

// labelSetsText introduces the label sets section; its verb is the
// size-only threshold.
const labelSetsText = "Every view scored under both label sets. The first is each corpus's first labels file, the one every other section of this report uses; the second is its second labels file. A view keeps the same commits under both, so only the verdicts differ: a view that sets capacity-only labels aside sets aside the commits the first set marks so, and a label with `agent: false` in the first set is dropped from both. The size-only rule stays at `sloc_delta > %d` and Advised applies this report's recommendations; both were chosen on the first set. A split-or-extraction label is `block` when a later commit had to split the package the commit grew past a ceiling, or extract the duplicate code it added (SPEC.md 11.3): what the gate claims to catch. Such a label's commit crossed a capacity `max`, grew a package already over one, or raised a duplicate-block count, which are rises the capacity and `dup_blocks` rules read, so under limits like the shipped ones the gate's recall on it is near 100%% by construction; the informative number under that set is the false-failure rate, the share of commits no later split or extraction undid that the gate failed.\n\n"
