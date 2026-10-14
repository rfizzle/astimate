// Package report renders the Markdown gate-validation report that
// calibration/validate writes: the gate's recall and false-failure rate on
// labeled commits against the SPEC.md 14 targets, each rule's precision
// and recall per corpus beside a size-only rule, each rule's sweep, a
// recommendation per rule, and the block commits the gate missed.
package report

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/calibration/replay/labels"
	"github.com/rfizzle/astimate/calibration/validate/internal/corpus"
	"github.com/rfizzle/astimate/calibration/validate/internal/measure"
)

// Targets are the SPEC.md 14 targets: fail at least Recall of block
// commits and at most FalseFailure of allow commits.
type Targets struct {
	Recall, FalseFailure float64
}

// Corpus describes one input corpus.
type Corpus struct {
	// Source names the corpus and its files.
	Source corpus.Source
	// Loaded is the corpus as joined.
	Loaded *corpus.Corpus
	// SetAside is the number of capacity-only block labels.
	SetAside int
	// Checked and Mismatched are measure.Scored.Recheck's counts.
	Checked, Mismatched int
	// SecondRule is the rule text of the corpus's second labels file,
	// Source.Second, empty for hand labels or without one.
	SecondRule string
}

// View is one corpus or pool of corpora as scored.
type View struct {
	Name string
	// Gate is the gate as replayed; Advised the gate with every
	// recommendation applied; Size the size-only rule at Input.SizeT.
	Gate, Advised, Size measure.Rates
	// Rules and Judged are each rule's rates and the commits with a row it
	// judges, indexed like Input.Rules.
	Rules  []measure.Rates
	Judged []int
	// WithCandidate holds, by the index in Input.Rules of each candidate
	// rule (measure.Rule.Candidate), the gate's rates with that candidate
	// added alone at its setting; nil without candidates.
	WithCandidate map[int]measure.Rates
}

// Missed lists one corpus's block commits the gate passed.
type Missed struct {
	Corpus  string
	Commits []*corpus.Commit
}

// Legacy is one capacity rule's firings on Basis and those that were
// growth in a package already over its max.
type Legacy struct {
	// Rule indexes Input.Rules.
	Rule        int
	Fired, Over measure.Rates
}

// Input is what the report describes.
type Input struct {
	// Date is the report's date; Version the configuration's
	// config_version and ConfigSource where it came from.
	Date, Version, ConfigSource string
	// CandidateSource is the configuration the candidate rules came from,
	// empty without candidates.
	CandidateSource string
	// Replayed lists the config_version of each replay whose rows were
	// gated under another configuration than Version, which measures as it
	// does (calibration/validate); the rules marked
	// measure.Rule.Reevaluated are those it ran at other limits.
	Replayed []string
	// Parts holds measure.Scored.Alone on the basis for each rule.
	Parts []measure.Rates
	// AgentOnly says labels with agent: false were dropped.
	AgentOnly bool
	Targets   Targets
	Corpora   []Corpus
	// Views are the corpora one by one, the set-aside views and the
	// pools; the last is the basis for the size threshold, the sweeps and
	// the advice.
	Views []View
	// Rules are the gated rules.
	Rules []measure.Rule
	// SizeT is the size-only threshold picked on the basis, SizeMet
	// whether it meets the false-failure target, SizeCurve the size-only
	// sweep there.
	SizeT     int
	SizeMet   bool
	SizeCurve []measure.SizePoint
	// Curves, Dropped and Advice are per rule, on the basis: its sweeps,
	// the gate's rates without it, and its recommendation.
	Curves   [][]measure.Curve
	Dropped  []measure.Rates
	Advice   []measure.Advice
	Criteria measure.Criteria
	// Legacy holds measure.Scored.Legacy on the basis for each capacity
	// rule.
	Legacy []Legacy
	Missed []Missed
	// Second holds, when the corpora name a second label set, each view of
	// Views scored under it: the same commits with the second set's
	// verdicts, with Gate, Advised and Size filled.
	Second []View
}

// basis is the view the sweeps and advice use.
func (in *Input) basis() *View { return &in.Views[len(in.Views)-1] }

// Render returns the report as Markdown.
func Render(in *Input) string {
	var b strings.Builder
	sections := []func(*strings.Builder, *Input){header, method, headline, labelSets, candidates, corpora, perRule, parts, size, sweeps, legacy, advice, missed}
	for _, section := range sections {
		section(&b, in)
	}
	return b.String()
}

// header writes the title and the inputs.
func header(b *strings.Builder, in *Input) {
	fmt.Fprintf(b, "# Gate validation %s\n\n- Configuration: `%s` (%s)\n", in.Date, in.Version, in.ConfigSource)
	for _, c := range in.Corpora {
		fmt.Fprintf(b, "- Corpus `%s`: rows `%s`, labels `%s`", c.Source.Name, c.Source.Data, c.Source.Labels)
		if c.Source.Second != "" {
			fmt.Fprintf(b, ", second labels `%s`", c.Source.Second)
		}
		b.WriteString("\n")
	}
	if in.CandidateSource != "" {
		fmt.Fprintf(b, "- Candidate rules from `%s`\n", in.CandidateSource)
	}
	b.WriteString("- Generated by `go run ./calibration/validate`\n\n")
}

// method writes how the numbers are computed.
func method(b *strings.Builder, in *Input) {
	agent := "every label is scored"
	if in.AgentOnly {
		agent = "a label with `agent: false` (a commit no coding agent took part in) is dropped, so the numbers describe agent-authored changes"
	}
	checked, mismatched := 0, 0
	for _, c := range in.Corpora {
		checked += c.Checked
		mismatched += c.Mismatched
	}
	ruleBlock, reverts := 0, 0
	for _, c := range in.Corpora {
		for i := range c.Loaded.Commits {
			cm := &c.Loaded.Commits[i]
			if !c.Loaded.RuleLabels || !cm.Block() {
				continue
			}
			ruleBlock++
			if slices.ContainsFunc(cm.Label.Fired, func(e labels.Evidence) bool { return e.Part == labels.PartRevert }) {
				reverts++
			}
		}
	}
	fmt.Fprintf(b, methodText, agent, pct(in.Targets.Recall), pct(in.Targets.FalseFailure), mismatched, checked,
		pct(in.Targets.FalseFailure), in.basis().Name, ruleBlock, reverts)
	if in.warnRules() {
		b.WriteString(warnMethodText)
	}
	if len(in.Replayed) > 0 {
		var names []string
		for _, r := range in.Rules {
			if r.Reevaluated {
				names = append(names, code(r.Name()))
			}
		}
		which := "none of its rules"
		if len(names) > 0 {
			which = strconv.Itoa(len(names)) + " of its rules, " + strings.Join(names, ", ") + ","
		}
		fmt.Fprintf(b, replayedMethodText, codes(in.Replayed), code(in.Version), which)
	}
	b.WriteString("\n")
}

// codes formats each of ss as code, joined by commas.
func codes(ss []string) string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = code(s)
	}
	return strings.Join(out, ", ")
}

// warnRules reports whether the configuration has a warn rule
// (gate.Threshold.Warns).
func (in *Input) warnRules() bool {
	return slices.ContainsFunc(in.Rules, func(r measure.Rule) bool { return r.Threshold.Warns() })
}

// targetHead is the header of targetCells' columns.
func targetHead(t Targets) []string {
	return []string{"Gate recall", "≥ " + pct(t.Recall), "Gate false failures", "≤ " + pct(t.FalseFailure),
		"Size-only recall", "Size-only false failures", "Advised recall", "Advised false failures"}
}

// targetCells formats v's gate rates against t, then its size-only and
// advised rates.
func targetCells(v *View, t Targets) []string {
	g := v.Gate
	return append([]string{frac(g.BlockFailed, g.Block), met(g.Recall() >= t.Recall),
		frac(g.AllowFailed, g.Allow), met(g.FalseFailure() <= t.FalseFailure),
		frac(v.Size.BlockFailed, v.Size.Block), frac(v.Size.AllowFailed, v.Size.Allow)}, shares(v.Advised)...)
}

// headline writes the gate's recall and false-failure rate per view.
func headline(b *strings.Builder, in *Input) {
	t := in.Targets
	fmt.Fprintf(b, "## Headline\n\nThe size-only rule is `sloc_delta > %d`.\n\n", in.SizeT)
	var rows [][]string
	for i := range in.Views {
		rows = append(rows, append([]string{in.Views[i].Name}, targetCells(&in.Views[i], t)...))
	}
	table(b, append([]string{"View"}, targetHead(t)...), rows)
	b.WriteString("Advised is the gate re-evaluated with every recommendation of this report applied (Recommendations), the other rules as recorded.\n\n")
	v := in.basis()
	fmt.Fprintf(b, "On the %s view the gate fails %s of `block` commits against a target of at least %s (%s) and %s of `allow` commits against a target of at most %s (%s). ",
		v.Name, pct(v.Gate.Recall()), pct(t.Recall), met(v.Gate.Recall() >= t.Recall),
		pct(v.Gate.FalseFailure()), pct(t.FalseFailure), met(v.Gate.FalseFailure() <= t.FalseFailure))
	fmt.Fprintf(b, "The size-only rule at %d SLOC fails %s and %s. With every recommendation below applied, the gate would fail %s and %s.\n\n",
		in.SizeT, pct(v.Size.Recall()), pct(v.Size.FalseFailure()), pct(v.Advised.Recall()), pct(v.Advised.FalseFailure()))
	top := in.SizeCurve[0]
	for _, p := range in.SizeCurve {
		if p.Rates.J() > top.Rates.J() {
			top = p
		}
	}
	fmt.Fprintf(b, "What the metrics add over the size of the change: the gate's J (recall minus false-failure rate) is %s; the size-only rule's best J is %s, at `sloc_delta > %d` (%s).\n\n",
		pts(v.Gate.J()), pts(top.Rates.J()), top.T, strings.Join(shares(top.Rates), " and "))
	var ruled []string
	for _, c := range in.Corpora {
		if c.Loaded.RuleLabels && !strings.Contains(c.Loaded.Rule, splitRulePhrase) {
			ruled = append(ruled, code(c.Source.Name))
		}
		if c.SetAside > 0 {
			block, _ := c.Loaded.Counts()
			fmt.Fprintf(b, "`%s` has %d capacity-only `block` labels. Its honest numbers are the view with them set aside, since the gate reads the same sizes those labels were drawn from; %d `block` labels remain there, so one commit moves its recall by %s points.\n\n",
				c.Source.Name, c.SetAside, block-c.SetAside, strconv.FormatFloat(100*share(1, block-c.SetAside), 'f', 1, 64))
		}
	}
	handSample(b, in)
	if len(ruled) > 0 {
		fmt.Fprintf(b, "The `block` labels of %s are fix-ups (Method): their recall is the share of changes a later fix touched that the gate failed, and their `allow` labels include every bad change nobody fixed within 20 commits, which inflates the false-failure rate by an unknown amount.\n\n", strings.Join(ruled, ", "))
	}
}

// corpora writes the per-corpus counts.
func corpora(b *strings.Builder, in *Input) {
	b.WriteString("## Corpora\n\n")
	rows := make([][]string, 0, len(in.Corpora))
	for _, c := range in.Corpora {
		block, allow := c.Loaded.Counts()
		kind := labelKind(c.Loaded.RuleLabels, c.Loaded.Rule)
		rows = append(rows, append([]string{c.Source.Name, kind}, ints(c.Loaded.Labeled, c.Loaded.NotLoaded, c.Loaded.NotAgent, block, allow, c.SetAside)...))
	}
	table(b, []string{"Corpus", "Labels", "Labeled", "Not loaded", "Dropped, not agent", "`block` scored", "`allow` scored", "Capacity-only `block`"}, rows)
}

// perRule writes one table per view with every rule and the size-only
// rule. A warn rule has its own row, but the gate's row counts only the
// rules that fail.
func perRule(b *strings.Builder, in *Input) {
	fmt.Fprintf(b, "## Per rule\n\nOne table for each of the %d views. Fired counts commits, not rows. A rule that judged no row of a view shows no rates.", len(in.Views))
	gateRow := "gate (any rule)"
	if in.warnRules() {
		gateRow = "gate (any `fail` rule)"
		b.WriteString(" A rule marked `(warn)` has `severity: warn`: it reports its breaches without failing the commit, so it has its own rates but is not part of the gate's row, which counts only the rules that fail.")
	}
	if len(candidateIndexes(in.Rules)) > 0 {
		b.WriteString(" A rule marked `(candidate)` is not in the configuration (Candidate rules): its rates are re-evaluated at its own setting, and the gate's row leaves it out.")
	}
	b.WriteString("\n\n")
	for _, v := range in.Views {
		fmt.Fprintf(b, "### %s\n\n", v.Name)
		rows := make([][]string, 0, len(in.Rules)+2)
		for i := range in.Rules {
			if v.Judged[i] == 0 {
				rows = append(rows, []string{code(ruleName(in.Rules, i)), "no rows", "", "", "", ""})
				continue
			}
			rows = append(rows, rateRow(code(ruleName(in.Rules, i)), v.Rules[i]))
		}
		rows = append(rows, rateRow(gateRow, v.Gate), rateRow("size-only, "+code("sloc_delta > "+strconv.Itoa(in.SizeT)), v.Size))
		table(b, []string{"Rule", "Fired on `block`", "Fired on `allow`", "Precision", "Recall", "False-failure share"}, rows)
	}
}

// parts writes each rule's part in the gate on the basis view: the
// commits it fired on and those no other rule that fails fired on, which
// the gate fails on its account alone (measure.Scored.Alone).
func parts(b *strings.Builder, in *Input) {
	v := in.basis()
	fmt.Fprintf(b, partsText, v.Name, v.Gate.AllowFailed)
	rows := make([][]string, 0, len(in.Rules))
	for i, r := range in.Rules {
		if r.Candidate || v.Judged[i] == 0 {
			continue
		}
		own, alone := v.Rules[i], in.Parts[i]
		share, aloneShare := "", ""
		if !r.Threshold.Warns() {
			share = frac(own.AllowFailed, v.Gate.AllowFailed)
			aloneShare = frac(alone.AllowFailed, v.Gate.AllowFailed)
		}
		rows = append(rows, []string{code(ruleName(in.Rules, i)), strconv.Itoa(own.BlockFailed), strconv.Itoa(alone.BlockFailed),
			strconv.Itoa(own.AllowFailed), strconv.Itoa(alone.AllowFailed), share, aloneShare})
	}
	table(b, []string{"Rule", "Fired on `block`", "Only it, `block`", "Fired on `allow`", "Only it, `allow`", "Share of the gate's false failures", "Only it, share"}, rows)
}

// handSample writes, for hand labels that mark the passed commits read
// as a sample and those left unread, how many of each the scored commits
// hold: recall counts only the block commits found among the commits
// read.
func handSample(b *strings.Builder, in *Input) {
	read, unread := 0, 0
	for _, c := range in.Corpora {
		for i := range c.Loaded.Commits {
			switch reason := c.Loaded.Commits[i].Label.Reason; {
			case strings.HasPrefix(reason, sampledReason):
				read++
			case strings.HasPrefix(reason, unreadReason):
				unread++
			}
		}
	}
	if read+unread == 0 {
		return
	}
	fmt.Fprintf(b, handSampleText, read+unread, read, unread)
}

// rateRow is one row of a per-rule table.
func rateRow(name string, r measure.Rates) []string {
	prec := "none fired"
	if p, ok := r.Precision(); ok {
		prec = pct(p)
	}
	return append([]string{name, strconv.Itoa(r.BlockFailed), strconv.Itoa(r.AllowFailed), prec}, shares(r)...)
}

// size writes the size-only sweep.
func size(b *strings.Builder, in *Input) {
	fmt.Fprintf(b, "## Size-only baseline\n\nOne rule on the change's size, swept on the %s view: fail a commit when a package's `sloc_delta` exceeds t.\n\n", in.basis().Name)
	rows := make([][]string, 0, len(in.SizeCurve))
	for _, p := range in.SizeCurve {
		rows = append(rows, append([]string{strconv.Itoa(p.T)}, append(shares(p.Rates), pts(p.Rates.J()))...))
	}
	table(b, []string{"t", "Recall", "False failures", "J"}, rows)
	if in.SizeMet {
		fmt.Fprintf(b, "Best t: %d, the highest recall with false failures at most %s.\n\n", in.SizeT, pct(in.Targets.FalseFailure))
		return
	}
	fmt.Fprintf(b, "No t keeps false failures at or under %s; t %d has the highest J.\n\n", pct(in.Targets.FalseFailure), in.SizeT)
}

// sweeps writes each rule's tradeoff tables.
func sweeps(b *strings.Builder, in *Input) {
	fmt.Fprintf(b, "## Sweeps\n\nOn the %s view. Recall and false-failure contribution are the rule's own; the gate columns hold every other rule as shipped. The shipped setting is marked with *.\n\n", in.basis().Name)
	for i := range in.Rules {
		fmt.Fprintf(b, "### %s\n\n", code(ruleName(in.Rules, i)))
		if len(in.Curves[i]) == 0 || in.basis().Judged[i] == 0 {
			b.WriteString("Nothing to sweep: no limit, or no row it judges.\n\n")
			continue
		}
		var rows [][]string
		for _, c := range in.Curves[i] {
			for _, p := range c.Points {
				name := p.Setting.String()
				if p.Shipped {
					name += " *"
				}
				rows = append(rows, append(append([]string{name}, shares(p.Rule)...), append([]string{pts(p.Rule.J())}, shares(p.Gate)...)...))
			}
		}
		rows = append(rows, append([]string{"rule dropped", "", "", ""}, shares(in.Dropped[i])...))
		table(b, []string{"Setting", "Recall contribution", "False-failure contribution", "J", "Gate recall", "Gate false failures"}, rows)
	}
}

// legacy writes how many capacity firings were growth in a package already
// over the ceiling.
func legacy(b *strings.Builder, in *Input) {
	if len(in.Legacy) == 0 {
		return
	}
	fmt.Fprintf(b, "## Capacity rules on packages already over the ceiling\n\n"+legacyText, in.basis().Name)
	rows := make([][]string, 0, len(in.Legacy))
	for _, l := range in.Legacy {
		counts := ints(l.Fired.Block, l.Fired.Allow, l.Over.Block, l.Over.Allow)
		rows = append(rows, append(append([]string{code(ruleName(in.Rules, l.Rule))}, counts...), frac(l.Over.Failed(), l.Fired.Failed())))
	}
	table(b, []string{"Rule", "Fired on `block`", "Fired on `allow`", "Already over, `block`", "Already over, `allow`", "Already over, share"}, rows)
}

// advice writes the recommendation per rule.
func advice(b *strings.Builder, in *Input) {
	fmt.Fprintf(b, "## Recommendations\n\n"+adviceText, in.basis().Name, in.Criteria.MinFired, pct(in.Criteria.Budget), pts(in.Criteria.MinJ))
	if len(candidateIndexes(in.Rules)) > 0 {
		b.WriteString("A candidate (Candidate rules) is judged by the same criteria, as if added at its setting: keep means add it as it is, retune add it at the setting named, and drop leave it out. Advised never adds one.\n\n")
	}
	rows := make([][]string, 0, len(in.Rules))
	for i := range in.Rules {
		a := in.Advice[i]
		to, why := "", a.Why
		if a.Action == measure.Retune {
			to = a.To.String()
		}
		for _, l := range in.Legacy {
			if l.Rule == i && a.Action != measure.Keep {
				why += "; " + pct(share(l.Over.Failed(), l.Fired.Failed())) + " of the commits it fired on only grew a package already over its max, which no retuned max prevents"
			}
		}
		rows = append(rows, []string{code(ruleName(in.Rules, i)), string(a.Action), to, why})
	}
	table(b, []string{"Rule", "Action", "To", "Why"}, rows)
}

// missed writes the block commits the gate passed, per corpus.
func missed(b *strings.Builder, in *Input) {
	b.WriteString("## Missed `block` commits\n\nThe `block` commits each corpus's gate passed, with the label's reason.\n\n")
	for _, m := range in.Missed {
		rows := make([][]string, len(m.Commits))
		for i, c := range m.Commits {
			rows[i] = []string{code(c.Hash[:min(12, len(c.Hash))]), c.Subject, c.Label.Reason}
		}
		b.WriteString("### " + m.Corpus + "\n\n")
		if len(rows) > 0 {
			table(b, []string{"Commit", "Subject", "Label reason"}, rows)
		} else {
			b.WriteString("None.\n\n")
		}
	}
}
