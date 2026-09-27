package report

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// Check is the outcome of one check run (SPEC.md section 8): the input of
// the check renderers.
type Check struct {
	// Packages are the checked packages, in the order they are rendered.
	Packages []CheckedPackage
	// Deleted are the module-relative directories that lost all their Go
	// files since the baseline. They appear in a summary line only, never as
	// violations (SPEC.md 8.4).
	Deleted []string
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

// Failed reports whether any package has a violation.
func (c *Check) Failed() bool {
	for i := range c.Packages {
		if len(c.Packages[i].Report.Violations) > 0 {
			return true
		}
	}
	return false
}

// ApplyGate records a gate outcome on r: the baseline block when base is
// non-nil (a package new at head has none), the violations and warnings of
// res, and the verdict.
func ApplyGate(r *Report, ref string, base *metrics.RawMetrics, res *gate.Result) {
	if base != nil {
		r.Baseline = &Baseline{Ref: ref, Metrics: *base}
	}
	r.Violations = findings(res.Violations)
	r.Warnings = findings(res.Warnings)
	passed := res.Passed
	r.Passed = &passed
}

// findings converts gate violations or warnings to report findings; the
// result is never nil, so the JSON array is present when a gate ran.
func findings(vs []gate.Violation) []Finding {
	out := make([]Finding, 0, len(vs))
	for i := range vs {
		v := &vs[i]
		f := Finding{Metric: v.Metric, Head: v.Head, Limit: v.Limit, Suggestion: v.Suggestion}
		if v.HasBase {
			b := v.Base
			f.Base = &b
		}
		out = append(out, f)
	}
	return out
}

// WriteCheckText writes c for a human reader: the violations, then the
// warnings, each grouped under its package, then one summary line per
// package with its agent passes, tier, finding counts and the change in
// agent passes from the baseline, and a final line naming deleted packages.
func WriteCheckText(w io.Writer, c *Check) error {
	bw := bufio.NewWriter(w)
	writeFindings(bw, c)
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
// each package's findings under its directory; an empty section is left
// out.
func writeFindings(w *bufio.Writer, c *Check) {
	sections := []struct {
		title string
		get   func(*Report) []Finding
	}{
		{"violations", func(r *Report) []Finding { return r.Violations }},
		{"warnings", func(r *Report) []Finding { return r.Warnings }},
	}
	for _, s := range sections {
		header := false
		for i := range c.Packages {
			r := &c.Packages[i].Report
			fs := s.get(r)
			if len(fs) == 0 {
				continue
			}
			if !header {
				_, _ = w.WriteString(s.title + ":\n")
				header = true
			}
			_, _ = w.WriteString("  " + r.PackagePath + "\n")
			for j := range fs {
				_, _ = w.WriteString("    " + findingText(&fs[j]) + "\n")
			}
		}
	}
}

// writeSummary writes one package's summary line.
func writeSummary(w *bufio.Writer, p *CheckedPackage) {
	r := &p.Report
	_, _ = fmt.Fprintf(w, "%s: %s passes (%s), %s, %s", r.PackagePath,
		strconv.FormatFloat(r.Rebuild.AgentPasses, 'f', 1, 64), r.Rebuild.Tier,
		plural(len(r.Violations), "violation", "violations"),
		plural(len(r.Warnings), "warning", "warnings"))
	if p.BaseAgentPasses != nil {
		d := round1(r.Rebuild.AgentPasses - *p.BaseAgentPasses)
		sign := ""
		if d >= 0 {
			// Normalize negative zero so no change prints as +0.0.
			d = math.Abs(d)
			sign = "+"
		}
		_, _ = w.WriteString(", " + sign + strconv.FormatFloat(d, 'f', 1, 64) + " passes from baseline")
	} else {
		_, _ = w.WriteString(", new since baseline")
	}
	_, _ = w.WriteString("\n")
}

// findingText renders one finding as "<metric>: <base> -> <head>, <limit>.
// <suggestion>", with "<head> (no baseline)" when there is no base value.
func findingText(f *Finding) string {
	var b strings.Builder
	b.WriteString(f.Metric)
	b.WriteString(": ")
	if f.Base != nil {
		b.WriteString(valueText(f.Metric, *f.Base))
		b.WriteString(" -> ")
		b.WriteString(valueText(f.Metric, f.Head))
	} else {
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

// WriteCheckJSON writes the packages' reports as an indented JSON array
// followed by a newline; no packages yields "[]".
func WriteCheckJSON(w io.Writer, c *Check) error {
	reports := make([]Report, 0, len(c.Packages))
	for i := range c.Packages {
		reports = append(reports, c.Packages[i].Report)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
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
// reason being the violations and warnings as text, and {} otherwise. When
// the check passes, any warnings are written as text to warnings instead,
// so w carries exactly one JSON object and nothing else.
func WriteHook(w, warnings io.Writer, c *Check) error {
	var text strings.Builder
	bw := bufio.NewWriter(&text)
	writeFindings(bw, c)
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
	data, err := json.Marshal(hookBlock{Decision: "block", Reason: text.String()})
	if err != nil {
		return fmt.Errorf("writing hook output: %w", err)
	}
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("writing hook output: %w", err)
	}
	return nil
}

// WriteGitHub writes GitHub Actions workflow commands (SPEC.md 8.5): one
// "::error file=<dir>::" annotation per violation and one
// "::warning file=<dir>::" per warning, package by package, with the
// property and message escaped per the workflow-command rules.
func WriteGitHub(w io.Writer, c *Check) error {
	bw := bufio.NewWriter(w)
	for i := range c.Packages {
		r := &c.Packages[i].Report
		file := escapeProperty(r.PackagePath)
		for j := range r.Violations {
			_, _ = bw.WriteString("::error file=" + file + "::" + escapeData(findingText(&r.Violations[j])) + "\n")
		}
		for j := range r.Warnings {
			_, _ = bw.WriteString("::warning file=" + file + "::" + escapeData(findingText(&r.Warnings[j])) + "\n")
		}
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("writing github annotations: %w", err)
	}
	return nil
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
