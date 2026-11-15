// Package explanation is the explain_metric tool of the MCP server (SPEC.md
// 10.1) less its registration: the tool's name, description and input, and
// its answer, a metric's definition, evidence and release with the loaded
// configuration's rules on it, or a word of the module report's glossary in
// the text its page prints. The server registers the tool and wraps an
// answer, or the failure to find one, in a tool result; this package holds
// what the tool says.
package explanation

import (
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/metrics/explain"
	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/report"
)

// ToolName is the name the explain tool is registered under (SPEC.md
// 10.1).
const ToolName = "explain_metric"

// Description tells the agent what explain_metric reports and when it
// helps.
const Description = "Explain one metric: what it counts, the evidence for reporting or gating it, " +
	"the release it ships in, and the threshold rules the server's loaded configuration applies to it. " +
	"Call it when a check_package violation or warning names a metric you do not understand, before " +
	"deciding how to fix it. It also explains the words of the module report in the same text its page " +
	"prints: sittings (the measured agent estimate), engineer_time (the textbook, unmeasured figure), split, watch, " +
	"copied, untested, changes, limits, hard_to_follow and paths."

// Input is the input of the explain_metric tool.
type Input struct {
	// Metric is the metric's JSON field name, such as dup_blocks.
	Metric string `json:"metric" jsonschema:"Metric name as it appears in reports and violations, such as dup_blocks or tokens_est."`
}

// glossaryLanguage is the language whose limits the report words are
// explained with: explain_metric names no package, and the shipped
// configuration is fitted for Go.
const glossaryLanguage = "go"

// Words returns the report words explain_metric answers besides the
// metrics, as the glossary of the module report prints them.
func Words() []string {
	return []string{"sittings", "engineer_time", "split", "watch", "copied", "untested", "changes", "limits", "hard_to_follow", "paths"}
}

// Explanation is the structured content of a successful explain_metric
// call.
type Explanation struct {
	Name string `json:"name"`
	// Term is the glossary entry's heading, as the report page prints it,
	// when Name is a report word rather than a metric; Definition is then
	// the page's text and the other fields are empty.
	Term       string      `json:"term,omitempty"`
	Definition string      `json:"definition"`
	Evidence   string      `json:"evidence"`
	Release    string      `json:"release"`
	Gated      bool        `json:"gated"`
	Thresholds []Threshold `json:"thresholds"`
	// MergedInto is the metric a finding of this one is attributed to
	// when both fire on one row in the same direction (SPEC.md 8.1);
	// omitted when the gate merges this metric into no other.
	MergedInto string `json:"merged_into,omitempty"`
	// RisesWhen is what must hold for a rule on this metric to fail a rise
	// against a baseline (SPEC.md 8.1); omitted when any rise is judged.
	RisesWhen string `json:"rises_when,omitempty"`
}

// Threshold is one loaded threshold rule on the explained metric.
type Threshold struct {
	Kind            string   `json:"kind"`
	Max             *float64 `json:"max,omitempty"`
	MaxDelta        *float64 `json:"max_delta,omitempty"`
	WarnAt          float64  `json:"warn_at,omitempty"`
	OverMaxDelta    *float64 `json:"over_max_delta,omitempty"`
	Floor           *float64 `json:"floor,omitempty"`
	Require         *bool    `json:"require,omitempty"`
	When            string   `json:"when,omitempty"`
	RatchetFromZero bool     `json:"ratchet_from_zero"`
	// Roles are the package roles the rule judges (SPEC.md 6.5); omitted
	// when it judges every role.
	Roles []string `json:"roles,omitempty"`
}

// For returns the explanation of name under cfg, which may be nil: a
// metric's, with cfg's rules on it, or a report word's, with the text the
// report page prints. It returns false when name is neither a metric nor a
// report word.
func For(cfg *config.Config, name string) (Explanation, bool) {
	e, ok := explain.Explain(name)
	if !ok {
		if term, found := narrate.Lookup(engine.Glossary(cfg, glossaryLanguage), name); found {
			return Explanation{Name: name, Term: term.Term, Definition: term.Text, Thresholds: []Threshold{}}, true
		}
		return Explanation{}, false
	}
	r := Explanation{
		Name:       name,
		Definition: e.Definition,
		Evidence:   e.Evidence,
		Release:    e.Release,
		Gated:      e.Gated,
		Thresholds: thresholdsFor(cfg, name),
	}
	if into, ok := report.MergedInto(name); ok {
		r.MergedInto = into
	}
	if cond, ok := report.RiseCondition(name); ok {
		r.RisesWhen = cond
	}
	return r, true
}

// thresholdsFor returns cfg's rules on metric, in file order, and an empty
// slice when there are none or no configuration.
func thresholdsFor(cfg *config.Config, metric string) []Threshold {
	out := []Threshold{}
	if cfg == nil {
		return out
	}
	for _, t := range cfg.Thresholds {
		if t.Metric != metric {
			continue
		}
		et := Threshold{
			Kind:            string(t.Kind),
			Max:             t.Max,
			MaxDelta:        t.MaxDelta,
			WarnAt:          t.WarnAt,
			OverMaxDelta:    t.OverMaxDelta,
			Require:         t.Require,
			RatchetFromZero: t.RatchetFromZero,
		}
		et.Floor, et.Roles = t.Floor, t.Roles
		if t.When != nil {
			et.When = t.When.Metric + " > " + formatFloat(t.When.Value)
		}
		out = append(out, et)
	}
	return out
}

// Text renders r for the agent: the name and release, the definition and
// evidence, one line per loaded threshold, when a rise is judged if not
// always, and which rule a finding of the metric is attributed to when the
// gate merges it into another's.
func (r Explanation) Text() string {
	if r.Term != "" {
		return r.Term + ": " + r.Definition + "\n"
	}
	var b strings.Builder
	b.WriteString(r.Name + " (" + r.Release)
	if r.Gated {
		b.WriteString(", gated by default)\n")
	} else {
		b.WriteString(", not gated by default)\n")
	}
	b.WriteString("Definition: " + r.Definition + "\n")
	b.WriteString("Evidence: " + r.Evidence + "\n")
	b.WriteString("Thresholds in the loaded config:")
	if len(r.Thresholds) == 0 {
		b.WriteString(" none\n")
	} else {
		b.WriteByte('\n')
		r.thresholdLines(&b)
	}
	if r.RisesWhen != "" {
		b.WriteString("Direction: against a baseline, a density rule on it fails a rise only when " + r.RisesWhen +
			", so a value that rises because code was deleted passes.\n")
	}
	if r.MergedInto != "" {
		b.WriteString("Merged: when " + r.MergedInto + " also fires on the row in the same direction, the two are one finding " +
			"attributed to " + r.MergedInto + ", whose limit names both and whose also list names " + r.Name + ".\n")
	}
	return b.String()
}

// thresholdLines writes one line per loaded threshold of r.
func (r Explanation) thresholdLines(b *strings.Builder) {
	for _, t := range r.Thresholds {
		b.WriteString("  " + t.Kind)
		if t.MaxDelta != nil {
			b.WriteString(" max_delta " + signedFloat(*t.MaxDelta))
		}
		if t.Max != nil {
			b.WriteString(" max " + formatFloat(*t.Max))
		}
		if t.WarnAt != 0 {
			b.WriteString(" warn_at " + formatFloat(t.WarnAt))
		}
		if t.OverMaxDelta != nil {
			b.WriteString(" over_max_delta " + formatFloat(*t.OverMaxDelta))
		}
		if t.Require != nil {
			b.WriteString(" require " + strconv.FormatBool(*t.Require))
		}
		if t.When != "" {
			b.WriteString(" when " + t.When)
		}
		if t.RatchetFromZero {
			b.WriteString(", ratchet from zero")
		}
		b.WriteString(t.overMaxText() + t.floorText() + t.rolesText())
		b.WriteByte('\n')
	}
}

// overMaxText says how a density rule's max judges a value over it
// (SPEC.md 8.1), as a clause led by "; ", and is empty for any other rule.
// A density rule with over_max_delta, which config accepts only on
// changed_func_cognitive_max, judges each changed function.
func (t Threshold) overMaxText() string {
	if t.Kind != "density" || t.Max == nil {
		return ""
	}
	if t.OverMaxDelta != nil {
		return "; max judges each changed function against its own baseline: a new function over it or one that " +
			"crossed it fails, and one already at or over it fails when it rose by more than over_max_delta"
	}
	if t.MaxDelta == nil {
		return "; max fails a new value over it and any rise over it"
	}
	return "; max fails a new package over it and a crossing, and a package already at or over it is judged by max_delta alone" +
		"; a change breaching both max_delta and max is one finding whose limit names both"
}

// floorText says what a density rule's floor does, as a clause led by
// "; ", and is empty for a rule without one.
func (t Threshold) floorText() string {
	if t.Floor == nil {
		return ""
	}
	return "; floor " + formatFloat(*t.Floor) + ": a value at or below it never fails, max included"
}

// rolesText says which package roles the rule judges, as a clause led by
// "; ", and is empty for a rule that judges every role.
func (t Threshold) rolesText() string {
	if len(t.Roles) == 0 {
		return ""
	}
	return "; judges " + strings.Join(t.Roles, ", ") + " packages only"
}

// signedFloat renders a delta with an explicit sign: "+2", "-1".
func signedFloat(v float64) string {
	if v < 0 {
		return formatFloat(v)
	}
	return "+" + formatFloat(v)
}

// formatFloat renders v in its shortest exact form.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}
