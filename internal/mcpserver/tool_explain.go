package mcpserver

import (
	"context"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/mcpserver/internal/explanation"
	"github.com/rfizzle/astimate/internal/metrics"
)

// addExplainTool registers explain_metric on srv, backed by s. The tool's
// name, description, input and answer are package explanation's.
func addExplainTool(srv *mcp.Server, s *session) {
	mcp.AddTool(srv, withOutputSchema[explanation.Explanation](&mcp.Tool{
		Name:        explanation.ToolName,
		Title:       "Explain a metric",
		Description: explanation.Description,
	}, s.opts.logger()), s.explainMetric)
}

// explainMetric handles an explain_metric call, answering from the loaded
// configuration (explanation.For). An unknown metric is a result with
// isError set whose text lists every valid name. Either way the result
// carries text content and structured content: the explanation, or
// {"error": ...}.
func (s *session) explainMetric(_ context.Context, _ *mcp.CallToolRequest, in explanation.Input) (*mcp.CallToolResult, any, error) {
	r, ok := explanation.For(s.opts.Config, in.Metric)
	if !ok {
		msg := explanation.ToolName + ": unknown metric " + strconv.Quote(in.Metric) +
			"; valid names: " + strings.Join(metrics.MetricNames(), ", ") +
			"; report words: " + strings.Join(explanation.Words(), ", ")
		s.opts.logger().Warn("tool call failed", "tool", explanation.ToolName, "metric", in.Metric)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: msg}},
			IsError: true,
		}, checkError{Error: msg}, nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: r.Text()}}}, r, nil
}
