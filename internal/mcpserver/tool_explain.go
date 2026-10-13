package mcpserver

import (
	"context"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

// explainToolName is the name the explain tool is registered under (SPEC.md
// 10.1).
const explainToolName = "explain_metric"

// explainToolDescription tells the agent what explain_metric reports and
// when it helps.
const explainToolDescription = "Explain one metric: what it counts, the evidence for reporting or gating it, " +
	"the release it ships in, and the threshold rules the server's loaded configuration applies to it. " +
	"Call it when a check_package violation or warning names a metric you do not understand, before " +
	"deciding how to fix it."

// ExplainInput is the input of the explain_metric tool.
type ExplainInput struct {
	// Metric is the metric's JSON field name, such as dup_blocks.
	Metric string `json:"metric" jsonschema:"Metric name as it appears in reports and violations, such as dup_blocks or tokens_est."`
}

// explainResult is the structured content of a successful explain_metric
// call.
type explainResult struct {
	Name       string             `json:"name"`
	Definition string             `json:"definition"`
	Evidence   string             `json:"evidence"`
	Release    string             `json:"release"`
	Gated      bool               `json:"gated"`
	Thresholds []explainThreshold `json:"thresholds"`
	// MergedInto is the metric a finding of this one is attributed to
	// when both fire on one row in the same direction (SPEC.md 8.1);
	// omitted when the gate merges this metric into no other.
	MergedInto string `json:"merged_into,omitempty"`
}

// explainThreshold is one loaded threshold rule on the explained metric.
type explainThreshold struct {
	Kind            string   `json:"kind"`
	Max             *float64 `json:"max,omitempty"`
	MaxDelta        *float64 `json:"max_delta,omitempty"`
	WarnAt          float64  `json:"warn_at,omitempty"`
	OverMaxDelta    *float64 `json:"over_max_delta,omitempty"`
	Require         *bool    `json:"require,omitempty"`
	When            string   `json:"when,omitempty"`
	RatchetFromZero bool     `json:"ratchet_from_zero"`
}

// addExplainTool registers explain_metric on srv, backed by s.
func addExplainTool(srv *mcp.Server, s *session) {
	mcp.AddTool(srv, withOutputSchema[explainResult](&mcp.Tool{
		Name:        explainToolName,
		Title:       "Explain a metric",
		Description: explainToolDescription,
	}, s.opts.logger()), s.explainMetric)
}

// explainMetric handles an explain_metric call. An unknown metric is a
// result with isError set whose text lists every valid name. Either way the
// result carries text content and structured content: the explanation, or
// {"error": ...}.
func (s *session) explainMetric(_ context.Context, _ *mcp.CallToolRequest, in ExplainInput) (*mcp.CallToolResult, any, error) {
	e, ok := metrics.Explain(in.Metric)
	if !ok {
		msg := explainToolName + ": unknown metric " + strconv.Quote(in.Metric) +
			"; valid names: " + strings.Join(metrics.MetricNames(), ", ")
		s.opts.logger().Warn("tool call failed", "tool", explainToolName, "metric", in.Metric)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: msg}},
			IsError: true,
		}, checkError{Error: msg}, nil
	}
	r := explainResult{
		Name:       in.Metric,
		Definition: e.Definition,
		Evidence:   e.Evidence,
		Release:    e.Release,
		Gated:      e.Gated,
		Thresholds: s.thresholdsFor(in.Metric),
	}
	if into, ok := report.MergedInto(in.Metric); ok {
		r.MergedInto = into
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: r.text()}}}, r, nil
}

// thresholdsFor returns the loaded configuration's rules on metric, in file
// order, and an empty slice when there are none or no configuration.
func (s *session) thresholdsFor(metric string) []explainThreshold {
	out := []explainThreshold{}
	if s.opts.Config == nil {
		return out
	}
	for _, t := range s.opts.Config.Thresholds {
		if t.Metric != metric {
			continue
		}
		et := explainThreshold{
			Kind:            string(t.Kind),
			Max:             t.Max,
			MaxDelta:        t.MaxDelta,
			WarnAt:          t.WarnAt,
			OverMaxDelta:    t.OverMaxDelta,
			Require:         t.Require,
			RatchetFromZero: t.RatchetFromZero,
		}
		if t.When != nil {
			et.When = t.When.Metric + " > " + formatFloat(t.When.Value)
		}
		out = append(out, et)
	}
	return out
}

// text renders r for the agent: the name and release, the definition and
// evidence, one line per loaded threshold, and which rule a finding of the
// metric is attributed to when the gate merges it into another's.
func (r explainResult) text() string {
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
	if r.MergedInto != "" {
		b.WriteString("Merged: when " + r.MergedInto + " also fires on the row in the same direction, the two are one finding " +
			"attributed to " + r.MergedInto + ", whose limit names both and whose also list names " + r.Name + ".\n")
	}
	return b.String()
}

// thresholdLines writes one line per loaded threshold of r.
func (r explainResult) thresholdLines(b *strings.Builder) {
	for _, t := range r.Thresholds {
		b.WriteString("  " + t.Kind)
		if t.MaxDelta != nil {
			b.WriteString(" max_delta +" + formatFloat(*t.MaxDelta))
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
		b.WriteString(t.overMaxText())
		b.WriteByte('\n')
	}
}

// overMaxText says how a density rule's max judges a value over it
// (SPEC.md 8.1), as a clause led by "; ", and is empty for any other rule.
func (t explainThreshold) overMaxText() string {
	if t.Kind != "density" || t.Max == nil {
		return ""
	}
	if t.MaxDelta == nil {
		return "; max fails a new value over it and any rise over it"
	}
	return "; max fails a new package over it and a crossing, and a package already at or over it is judged by max_delta alone" +
		"; a change breaching both max_delta and max is one finding whose limit names both"
}

// formatFloat renders v in its shortest exact form.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}
