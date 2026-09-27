package mcpserver

import (
	"context"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/metrics"
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
}

// explainThreshold is one loaded threshold rule on the explained metric.
type explainThreshold struct {
	Kind            string   `json:"kind"`
	Max             *float64 `json:"max,omitempty"`
	MaxDelta        *float64 `json:"max_delta,omitempty"`
	WarnAt          float64  `json:"warn_at,omitempty"`
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
// evidence, then one line per loaded threshold.
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
		return b.String()
	}
	b.WriteByte('\n')
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
		if t.Require != nil {
			b.WriteString(" require " + strconv.FormatBool(*t.Require))
		}
		if t.When != "" {
			b.WriteString(" when " + t.When)
		}
		if t.RatchetFromZero {
			b.WriteString(", ratchet from zero")
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// formatFloat renders v in its shortest exact form.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}
