package report

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"path"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// Check is the outcome of one check run (SPEC.md section 8): the input of
// the check renderers.
type Check struct {
	// Module is the module-level row, with package path
	// metrics.ModuleRowID: module-wide metrics such as the number of
	// distinct cross-package duplicate blocks, gated by the rules on those
	// metrics only. It is rendered before the packages. Nil when the check
	// has none, as for a check that selected no package or an extractor
	// without module metrics.
	Module *CheckedPackage
	// Packages are the checked packages, in the order they are rendered.
	Packages []CheckedPackage
	// Deleted are the module-relative directories that lost all their Go
	// files since the baseline. They appear in a summary line only, never as
	// violations (SPEC.md 8.4).
	Deleted []string
	// ModuleDir is the module root's directory relative to the top level
	// of the repository holding it, in slash form; empty when the module
	// root is the top level or in no repository. WriteGitHub prefixes
	// every file it annotates with it, since GitHub resolves annotation
	// paths from the repository root.
	ModuleDir string
	// Tokenizer is the tokenizer the check counted tokens with, named in
	// the line saying a baseline's token counts are not comparable.
	Tokenizer string
}

// CheckedPackage is one checked package: its report with the gate outcome
// applied, and the rebuild estimate at the baseline for the summary delta.
type CheckedPackage struct {
	// Report is the SPEC.md 10.2 report, with Passed set.
	Report Report
	// BaseAgentPasses is agent_passes at the baseline, rounded as the
	// report's is; nil when the package has no baseline.
	BaseAgentPasses *float64
}

// Failed reports whether the module row or any package has a violation.
func (c *Check) Failed() bool {
	for _, p := range c.rows() {
		if len(p.Report.Violations) > 0 {
			return true
		}
	}
	return false
}

// rows returns the module row, when there is one, followed by the
// packages: the order every renderer lists them in.
func (c *Check) rows() []*CheckedPackage {
	rows := make([]*CheckedPackage, 0, len(c.Packages)+1)
	if c.Module != nil {
		rows = append(rows, c.Module)
	}
	for i := range c.Packages {
		rows = append(rows, &c.Packages[i])
	}
	return rows
}

// ApplyGate records a gate outcome on r: the baseline block when base is
// non-nil (a package new at head has none), the violations, warnings and
// exempted violations of res, and the verdict.
func ApplyGate(r *Report, ref string, base *metrics.RawMetrics, res *gate.Result) {
	if base != nil {
		r.Baseline = &Baseline{Ref: ref, Metrics: *base}
	}
	r.Violations = findings(res.Violations)
	r.Warnings = findings(res.Warnings)
	r.Exemptions = make([]Exempted, 0, len(res.Exempted))
	for i := range res.Exempted {
		e := &res.Exempted[i]
		r.Exemptions = append(r.Exemptions, Exempted{Finding: finding(&e.Violation), Reason: e.Reason})
	}
	passed := res.Passed
	r.Passed = &passed
}

// MarkTokenizer records on r's baseline block, if it has one, the
// tokenizer base the baseline counted tokens with and whether it is check,
// the tokenizer of the check, so consumers of the report know when token
// deltas are not comparable.
func MarkTokenizer(r *Report, base, check string) {
	if r.Baseline == nil {
		return
	}
	r.Baseline.Tokenizer = base
	r.Baseline.TokensComparable = base == check
}

// tokenizerNote returns the line saying token counts are not comparable
// when a row's baseline counted tokens with another tokenizer than the
// check did, and "" otherwise.
func tokenizerNote(c *Check) string {
	for _, p := range c.rows() {
		if b := p.Report.Baseline; b != nil && b.Tokenizer != "" && !b.TokensComparable {
			return "baseline tokenizer " + b.Tokenizer + " differs from check tokenizer " + c.Tokenizer +
				"; token counts are not comparable"
		}
	}
	return ""
}

// findings converts gate violations or warnings to report findings; the
// result is never nil, so the JSON array is present when a gate ran.
func findings(vs []gate.Violation) []Finding {
	out := make([]Finding, 0, len(vs))
	for i := range vs {
		out = append(out, finding(&vs[i]))
	}
	return out
}

// finding converts one gate violation or warning to a report finding.
func finding(v *gate.Violation) Finding {
	f := Finding{Metric: v.Metric, Head: v.Head, Limit: v.Limit, Suggestion: v.Suggestion, Severity: string(v.Severity)}
	if v.HasBase {
		b := v.Base
		f.Base = &b
	}
	return f
}

// WriteCheckText writes c for a human reader: the violations, then the
// warnings, then the exempted violations with their reasons, each grouped
// under its package (the module row's first, under its id,
// metrics.ModuleRowID), then one summary line for the module row and one
// per package with its finding counts (writeSummary says when the agent
// passes, tier and change from the baseline join them), and a final line
// naming deleted packages.
func WriteCheckText(w io.Writer, c *Check) error {
	bw := bufio.NewWriter(w)
	writeFindings(bw, c)
	if c.Module != nil {
		writeModuleSummary(bw, c.Module)
	}
	for i := range c.Packages {
		writeSummary(bw, &c.Packages[i])
	}
	if len(c.Deleted) > 0 {
		_, _ = bw.WriteString("deleted since baseline: " + strings.Join(c.Deleted, ", ") + "\n")
	}
	if len(c.Packages) == 0 && len(c.Deleted) == 0 {
		_, _ = bw.WriteString("no changed packages\n")
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("writing check report: %w", err)
	}
	return nil
}

// writeFindings writes the violations section, then the warnings section,
// then the exempted section, the module row's findings under
// metrics.ModuleRowID and then each package's under its directory; an
// empty section is left out. Each exempted line ends with its reason
// (exemptedText), so no silenced finding is shown without it.
func writeFindings(w *bufio.Writer, c *Check) {
	sections := []struct {
		title string
		lines func(*Report) []string
	}{
		{"violations", func(r *Report) []string { return findingLines(r.Violations) }},
		{"warnings", func(r *Report) []string { return findingLines(r.Warnings) }},
		{"exempted", func(r *Report) []string {
			lines := make([]string, 0, len(r.Exemptions))
			for j := range r.Exemptions {
				lines = append(lines, exemptedText(&r.Exemptions[j]))
			}
			return lines
		}},
	}
	for _, s := range sections {
		header := false
		for _, p := range c.rows() {
			r := &p.Report
			lines := s.lines(r)
			if len(lines) == 0 {
				continue
			}
			if !header {
				_, _ = w.WriteString(s.title + ":\n")
				header = true
			}
			_, _ = w.WriteString("  " + r.PackagePath + "\n")
			for _, l := range lines {
				_, _ = w.WriteString("    " + l + "\n")
			}
		}
	}
}

// findingLines renders each of fs with findingText.
func findingLines(fs []Finding) []string {
	lines := make([]string, 0, len(fs))
	for j := range fs {
		lines = append(lines, findingText(&fs[j]))
	}
	return lines
}

// exemptedText renders an exempted violation as its finding line
// (findingText) followed by "Exempted: <reason>".
func exemptedText(e *Exempted) string {
	return findingText(&e.Finding) + " Exempted: " + e.Reason
}

// findingCounts renders a row's finding counts: "N violations, M warnings,
// K exempted".
func findingCounts(r *Report) string {
	return plural(len(r.Violations), "violation", "violations") + ", " +
		plural(len(r.Warnings), "warning", "warnings") + ", " +
		strconv.Itoa(len(r.Exemptions)) + " exempted"
}

// writeSummary writes one package's summary line: "<pkg>: N violations,
// M warnings, K exempted" (findingCounts) and whether the package is new since the baseline. The agent
// passes and tier lead the counts, and the change in agent passes from the
// baseline follows them, only when the rebuild estimate is calibrated
// (Rebuild.Calibrated): until then the estimate's invariants (SPEC.md 7.5)
// can contradict the gate, rating a duplicated package one pass while the
// gate asks for a split, so gate output leaves the estimate to assess and
// rank.
func writeSummary(w *bufio.Writer, p *CheckedPackage) {
	r := &p.Report
	calibrated := r.Rebuild.Calibrated
	_, _ = w.WriteString(r.PackagePath + ": ")
	if calibrated {
		_, _ = w.WriteString(strconv.FormatFloat(r.Rebuild.AgentPasses, 'f', 1, 64) + " passes (" +
			string(r.Rebuild.Tier) + "), ")
	}
	_, _ = w.WriteString(findingCounts(r))
	switch {
	case p.BaseAgentPasses == nil:
		_, _ = w.WriteString(", new since baseline")
	case calibrated:
		d := round1(r.Rebuild.AgentPasses - *p.BaseAgentPasses)
		sign := ""
		if d >= 0 {
			// Normalize negative zero so no change prints as +0.0.
			d = math.Abs(d)
			sign = "+"
		}
		_, _ = w.WriteString(", " + sign + strconv.FormatFloat(d, 'f', 1, 64) + " passes from baseline")
	}
	_, _ = w.WriteString("\n")
}

// writeModuleSummary writes the module row's summary line: the module-wide
// metrics it carries, its finding counts and whether the baseline has a
// module row. It has no rebuild estimate to report.
func writeModuleSummary(w *bufio.Writer, p *CheckedPackage) {
	r := &p.Report
	_, _ = w.WriteString(r.PackagePath + ":")
	for _, name := range metrics.MetricNames() {
		v, ok := r.Metrics.Value(name)
		if !ok || isV0(name) {
			continue
		}
		_, _ = w.WriteString(" " + name + " " + valueText(name, v) + ",")
	}
	_, _ = w.WriteString(" " + findingCounts(r))
	if r.Baseline == nil {
		_, _ = w.WriteString(", new since baseline")
	}
	_, _ = w.WriteString("\n")
}

// isV0 reports whether name is a v0 metric, which the module row leaves at
// zero: v0 fields are the ones a zero RawMetrics reports a value for.
func isV0(name string) bool {
	var zero metrics.RawMetrics
	_, ok := zero.Value(name)
	return ok
}

// findingText renders one finding as "<metric>: <base> -> <head>, <limit>.
// <suggestion>", with "<head> (no baseline)" when there is no base value.
// changed_func_cognitive_max is itself a diff against the baseline and
// never has a base value, so it shows "<head> (changed since baseline)".
func findingText(f *Finding) string {
	var b strings.Builder
	b.WriteString(f.Metric)
	b.WriteString(": ")
	switch {
	case f.Base != nil:
		b.WriteString(valueText(f.Metric, *f.Base))
		b.WriteString(" -> ")
		b.WriteString(valueText(f.Metric, f.Head))
	case f.Metric == "changed_func_cognitive_max":
		b.WriteString(valueText(f.Metric, f.Head))
		b.WriteString(" (changed since baseline)")
	default:
		b.WriteString(valueText(f.Metric, f.Head))
		b.WriteString(" (no baseline)")
	}
	b.WriteString(", ")
	b.WriteString(f.Limit)
	b.WriteString(".")
	if f.Suggestion != "" {
		b.WriteString(" ")
		b.WriteString(f.Suggestion)
	}
	return b.String()
}

// valueText formats a metric value as the table does: booleans as true or
// false, numbers to at most two decimals.
func valueText(metric string, v float64) string {
	switch metric {
	case "has_tests", "uses_cgo", "uses_reflect":
		return strconv.FormatBool(v != 0)
	}
	return formatFloat(math.Round(v*100) / 100)
}

// plural renders n with the singular or plural noun.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// WriteCheckJSON writes the reports of the module row, when there is one,
// and the packages as an indented JSON array followed by a newline; the
// module row is an ordinary entry whose package_path is
// metrics.ModuleRowID, "<module>", written without HTML escaping. No rows
// yields "[]".
func WriteCheckJSON(w io.Writer, c *Check) error {
	rows := c.rows()
	reports := make([]Report, 0, len(rows))
	for _, p := range rows {
		reports = append(reports, p.Report)
	}
	enc := newEncoder(w, "  ")
	if err := enc.Encode(reports); err != nil {
		return fmt.Errorf("writing json check report: %w", err)
	}
	return nil
}

// hookBlock is the Claude Code Stop hook decision that keeps the agent
// working.
type hookBlock struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// WriteHook writes the Claude Code Stop hook output (SPEC.md 8.5) to w:
// {"decision":"block","reason":...} when any package has a violation, the
// reason being the violations, warnings and exempted violations, each of
// the last with its reason, as text, and {} otherwise. When the check
// passes, any warnings and exempted violations are written as text to
// warnings instead, so w carries exactly one JSON object and nothing else. A baseline whose
// token counts are not comparable adds a "warning: " line saying so.
func WriteHook(w, warnings io.Writer, c *Check) error {
	var text strings.Builder
	bw := bufio.NewWriter(&text)
	writeFindings(bw, c)
	if note := tokenizerNote(c); note != "" {
		_, _ = bw.WriteString("warning: " + note + "\n")
	}
	_ = bw.Flush() // a strings.Builder never fails a write

	if !c.Failed() {
		if _, err := io.WriteString(w, "{}\n"); err != nil {
			return fmt.Errorf("writing hook output: %w", err)
		}
		if text.Len() == 0 {
			return nil
		}
		if _, err := io.WriteString(warnings, text.String()); err != nil {
			return fmt.Errorf("writing hook warnings: %w", err)
		}
		return nil
	}
	// newEncoder does not HTML-escape; with escaping the module row's
	// heading, "<module>", would reach the reason as "\u003cmodule\u003e".
	enc := newEncoder(w, "")
	if err := enc.Encode(hookBlock{Decision: "block", Reason: text.String()}); err != nil {
		return fmt.Errorf("writing hook output: %w", err)
	}
	return nil
}

// githubModuleLead leads the message of each of the module row's GitHub
// annotations. It names the row in words rather than by metrics.ModuleRowID:
// an annotation is read by a person, and carries no package path to match.
const githubModuleLead = "module: "

// WriteGitHub writes GitHub Actions workflow commands (SPEC.md 8.5): one
// "::error" annotation per violation, one "::warning" per warning and one
// "::notice" per exempted violation, whose message ends with the
// exemption's reason (exemptedText), package by package, with the property and message escaped per the
// workflow-command rules. A finding with a location (Finding.Location)
// is annotated "file=<file>,line=<line>" on it, so it lands on the
// line that caused it; a package finding without one is annotated
// "file=<dir>" on its package directory. Every file is prefixed with
// c.ModuleDir, so paths are relative to the repository top level. The
// module row's findings come first and their message leads with
// "module: "; the module row has no directory, so one of its findings
// without a location carries no file. A baseline whose token counts are
// not comparable adds one "::warning title=astimate::" line saying so,
// before the findings.
func WriteGitHub(w io.Writer, c *Check) error {
	bw := bufio.NewWriter(w)
	if note := tokenizerNote(c); note != "" {
		_, _ = bw.WriteString("::warning title=astimate::" + escapeData(note) + "\n")
	}
	for _, p := range c.rows() {
		r := &p.Report
		prop, lead := " file="+escapeProperty(c.repoPath(r.PackagePath)), ""
		if p == c.Module {
			prop, lead = "", githubModuleLead
		}
		for j := range r.Violations {
			f := &r.Violations[j]
			_, _ = bw.WriteString("::error" + c.findingProperty(prop, f) + "::" + escapeData(lead+findingText(f)) + "\n")
		}
		for j := range r.Warnings {
			f := &r.Warnings[j]
			_, _ = bw.WriteString("::warning" + c.findingProperty(prop, f) + "::" + escapeData(lead+findingText(f)) + "\n")
		}
		for j := range r.Exemptions {
			e := &r.Exemptions[j]
			_, _ = bw.WriteString("::notice" + c.findingProperty(prop, &e.Finding) + "::" + escapeData(lead+exemptedText(e)) + "\n")
		}
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("writing github annotations: %w", err)
	}
	return nil
}

// repoPath returns the module-relative path p relative to the repository
// top level: p prefixed with c.ModuleDir.
func (c *Check) repoPath(p string) string {
	if c.ModuleDir == "" {
		return p
	}
	return path.Join(c.ModuleDir, p)
}

// findingProperty returns the annotation properties of f: its own file,
// relative to the repository top level, and line when it has a location,
// and prop, its row's, otherwise.
func (c *Check) findingProperty(prop string, f *Finding) string {
	loc := f.Location
	if loc == nil || loc.File == "" {
		return prop
	}
	s := " file=" + escapeProperty(c.repoPath(loc.File))
	if loc.Line > 0 {
		s += ",line=" + strconv.Itoa(loc.Line)
	}
	return s
}

// escapeData escapes a workflow-command message: %, \r and \n.
func escapeData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

// escapeProperty escapes a workflow-command property value: the message
// escapes plus : and ,.
func escapeProperty(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(s)
}
