package mcpserver

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/metrics"
)

// callExplain calls explain_metric for metric and returns the result.
func callExplain(t *testing.T, cs *mcp.ClientSession, metric string) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      explainToolName,
		Arguments: map[string]any{"metric": metric},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	return res
}

// decodeExplain decodes the structured content of res as an explanation,
// rejecting unknown fields.
func decodeExplain(t *testing.T, res *mcp.CallToolResult) explainResult {
	t.Helper()
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("encoding structured content: %v", err)
	}
	var r explainResult
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

func TestExplainMetricThresholdsFollowConfig(t *testing.T) {
	t.Parallel()

	const defaultRule = "metric: dup_blocks\n    kind: density\n    max_delta: 0\n"
	src := string(config.Default())
	if !strings.Contains(src, defaultRule) {
		t.Fatalf("default config lacks %q", defaultRule)
	}
	custom, err := config.Parse([]byte(strings.Replace(src, defaultRule,
		"metric: dup_blocks\n    kind: density\n    max_delta: 2\n", 1)))
	if err != nil {
		t.Fatalf("parsing custom config: %v", err)
	}

	tests := []struct {
		name      string
		cfg       *config.Config
		wantDelta float64
		wantText  string
	}{
		{name: "default", cfg: defaultConfig(t), wantDelta: 0, wantText: "density max_delta +0, ratchet from zero"},
		{name: "custom", cfg: custom, wantDelta: 2, wantText: "density max_delta +2, ratchet from zero"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cs := newTestClient(t, Options{Config: tt.cfg})
			res := callExplain(t, cs, "dup_blocks")
			text := resultText(res)
			if res.IsError {
				t.Fatalf("IsError = true; text:\n%s", text)
			}
			r := decodeExplain(t, res)
			if len(r.Thresholds) == 0 {
				t.Fatalf("thresholds empty; text:\n%s", text)
			}
			th := r.Thresholds[0]
			if th.Kind != "density" || th.MaxDelta == nil || *th.MaxDelta != tt.wantDelta || !th.RatchetFromZero {
				t.Errorf("thresholds[0] = %+v, want density, max_delta %v, ratchet_from_zero", th, tt.wantDelta)
			}
			if !strings.Contains(text, tt.wantText) {
				t.Errorf("text = %q, want %q", text, tt.wantText)
			}
		})
	}
}

// TestExplainMetricOverMaxDelta checks a capacity rule's over_max_delta
// reaches the structured and text output of explain_metric.
func TestExplainMetricOverMaxDelta(t *testing.T) {
	t.Parallel()

	cs := newTestClient(t, Options{Config: defaultConfig(t)})
	res := callExplain(t, cs, "sloc")
	text := resultText(res)
	r := decodeExplain(t, res)
	if len(r.Thresholds) == 0 {
		t.Fatalf("thresholds empty; text:\n%s", text)
	}
	if th := r.Thresholds[0]; th.Kind != "capacity" || th.OverMaxDelta == nil || *th.OverMaxDelta != 100 {
		t.Errorf("thresholds[0] = %+v, want capacity with over_max_delta 100", th)
	}
	if want := "capacity max 1000 warn_at 0.75 over_max_delta 100"; !strings.Contains(text, want) {
		t.Errorf("text = %q, want %q", text, want)
	}
}

func TestExplainMetricNoConfig(t *testing.T) {
	t.Parallel()

	cs := newTestClient(t, Options{})
	res := callExplain(t, cs, "sloc")
	if res.IsError {
		t.Fatalf("IsError = true; text:\n%s", resultText(res))
	}
	if r := decodeExplain(t, res); len(r.Thresholds) != 0 {
		t.Errorf("thresholds = %+v, want none without a config", r.Thresholds)
	}
	if text := resultText(res); !strings.Contains(text, "Thresholds in the loaded config: none") {
		t.Errorf("text = %q, want no thresholds reported", text)
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
