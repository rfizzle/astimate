package mcpserver

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/mcpserver/internal/explanation"
	"github.com/rfizzle/astimate/internal/metrics"
)

// callExplain calls explain_metric for metric and returns the result.
func callExplain(t *testing.T, cs *mcp.ClientSession, metric string) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      explanation.ToolName,
		Arguments: map[string]any{"metric": metric},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	return res
}

// decodeExplain decodes the structured content of res as an explanation,
// rejecting unknown fields.
func decodeExplain(t *testing.T, res *mcp.CallToolResult) explanation.Explanation {
	t.Helper()
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("encoding structured content: %v", err)
	}
	var r explanation.Explanation
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("decoding structured content as an explanation: %v\n%s", err, data)
	}
	if r.Thresholds == nil {
		t.Errorf("structured content = %s, want thresholds as an array", data)
	}
	return r
}

// TestExplainMetricEveryName calls the tool for every metric name; the
// reflection test in internal/metrics proves MetricNames matches RawMetrics.
func TestExplainMetricEveryName(t *testing.T) {
	t.Parallel()

	cs := newTestClient(t, Options{Config: defaultConfig(t)})
	for _, name := range metrics.MetricNames() {
		t.Run(name, func(t *testing.T) {
			res := callExplain(t, cs, name)
			text := resultText(res)
			if res.IsError {
				t.Fatalf("IsError = true; text:\n%s", text)
			}
			r := decodeExplain(t, res)
			if r.Name != name || r.Definition == "" || r.Evidence == "" || r.Release == "" {
				t.Errorf("explanation = %+v, want name, definition, evidence and release", r)
			}
			if !strings.HasPrefix(text, name+" (") || !strings.Contains(text, r.Definition) {
				t.Errorf("text = %q, want the name and definition", text)
			}
			if r.Gated != (len(r.Thresholds) > 0) {
				t.Errorf("gated = %v with %d default thresholds; the two disagree", r.Gated, len(r.Thresholds))
			}
		})
	}
}

func TestExplainMetricUnknown(t *testing.T) {
	t.Parallel()

	cs := newTestClient(t, Options{Config: defaultConfig(t)})
	for _, name := range []string{"no_such_metric", ""} {
		t.Run(name, func(t *testing.T) {
			res := callExplain(t, cs, name)
			text := resultText(res)
			if !res.IsError {
				t.Fatalf("IsError = false for %q; text:\n%s", name, text)
			}
			for _, valid := range metrics.MetricNames() {
				if !strings.Contains(text, valid) {
					t.Errorf("text lacks valid name %q:\n%s", valid, text)
				}
			}
			data, err := json.Marshal(res.StructuredContent)
			if err != nil {
				t.Fatal(err)
			}
			var e checkError
			if err := json.Unmarshal(data, &e); err != nil || e.Error != text {
				t.Errorf("structured content = %s, want {\"error\": text}", data)
			}
		})
	}
}
