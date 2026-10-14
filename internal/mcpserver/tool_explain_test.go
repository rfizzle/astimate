package mcpserver

import (
	"bytes"
	"encoding/json"
	"slices"
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

func TestSignedFloat(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		v    float64
		want string
	}{{0, "+0"}, {2, "+2"}, {1.5, "+1.5"}, {-1, "-1"}} {
		if got := signedFloat(tt.v); got != tt.want {
			t.Errorf("signedFloat(%v) = %q, want %q", tt.v, got, tt.want)
		}
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

// TestExplainMetricRoles checks explain_metric says which package roles
// each rule judges: internal_imports every role but main, and has_tests
// one rule for main past 300 SLOC beside the rule for the others.
func TestExplainMetricRoles(t *testing.T) {
	t.Parallel()

	cs := newTestClient(t, Options{Config: defaultConfig(t)})
	res := callExplain(t, cs, "internal_imports")
	text := resultText(res)
	r := decodeExplain(t, res)
	if len(r.Thresholds) != 1 || !slices.Equal(r.Thresholds[0].Roles, []string{"library", "test-support", "generated-only"}) {
		t.Errorf("thresholds = %+v, want one judging library, test-support and generated-only", r.Thresholds)
	}
	if want := "; judges library, test-support, generated-only packages only\n"; !strings.Contains(text, want) {
		t.Errorf("text = %q, want %q", text, want)
	}
	res = callExplain(t, cs, "has_tests")
	text = resultText(res)
	if want := "requirement require true when sloc > 300; judges main packages only\n"; !strings.Contains(text, want) {
		t.Errorf("text = %q, want %q", text, want)
	}
	res = callExplain(t, cs, "globals")
	if text, r := resultText(res), decodeExplain(t, res); strings.Contains(text, "judges") || len(r.Thresholds[0].Roles) != 0 {
		t.Errorf("globals text = %q, roles %v; want a rule judging every role, unmentioned", text, r.Thresholds[0].Roles)
	}
}

// TestExplainMetricDensityOverMax checks explain_metric says how a density
// rule's max judges a value over it, with and without a max_delta.
func TestExplainMetricDensityOverMax(t *testing.T) {
	t.Parallel()

	cs := newTestClient(t, Options{Config: defaultConfig(t)})
	for metric, want := range map[string]string{
		"cognitive_p90": "density max_delta +3 max 20; max fails a new package over it and a crossing, " +
			"and a package already at or over it is judged by max_delta alone",
		"changed_func_cognitive_max": "density max 50 over_max_delta 5; max judges each changed function against its own baseline: " +
			"a new function over it or one that crossed it fails, and one already at or over it fails when it rose by more than over_max_delta",
		"dup_blocks": "density max_delta +0, ratchet from zero\n",
	} {
		if text := resultText(callExplain(t, cs, metric)); !strings.Contains(text, want) {
			t.Errorf("%s: text = %q, want %q", metric, text, want)
		}
	}
}

// TestExplainMetricFloorAndDirection checks explain_metric reports a
// density rule's floor and, for the ratio and percentile metrics, when a
// rise is judged (SPEC.md 8.1); a metric without either says neither.
func TestExplainMetricFloorAndDirection(t *testing.T) {
	t.Parallel()

	cs := newTestClient(t, Options{Config: defaultConfig(t)})
	for metric, want := range map[string]struct {
		floor float64
		rises string
	}{
		"max_nesting":     {floor: 4},
		"cognitive_p90":   {floor: 10, rises: "a function changed since the baseline is more complex than the baseline's p90"},
		"duplication_pct": {floor: 10, rises: "duplicated_lines rose"},
		"dup_blocks":      {},
	} {
		res := callExplain(t, cs, metric)
		text := resultText(res)
		r := decodeExplain(t, res)
		if len(r.Thresholds) == 0 {
			t.Fatalf("%s: thresholds empty; text:\n%s", metric, text)
		}
		th := r.Thresholds[0]
		switch {
		case want.floor == 0 && th.Floor != nil:
			t.Errorf("%s: floor = %v, want none", metric, *th.Floor)
		case want.floor != 0 && (th.Floor == nil || *th.Floor != want.floor):
			t.Errorf("%s: floor = %v, want %v", metric, th.Floor, want.floor)
		}
		hasFloorText := strings.Contains(text, "; floor "+formatFloat(want.floor)+": a value at or below it never fails, max included\n")
		if hasFloorText != (want.floor != 0) {
			t.Errorf("%s: text = %q, floor clause present %v, want %v", metric, text, hasFloorText, want.floor != 0)
		}
		if r.RisesWhen != want.rises {
			t.Errorf("%s: rises_when = %q, want %q", metric, r.RisesWhen, want.rises)
		}
		line := "Direction: against a baseline, a density rule on it fails a rise only when " + want.rises
		if got := strings.Contains(text, "Direction:"); got != (want.rises != "") || (got && !strings.Contains(text, line)) {
			t.Errorf("%s: text = %q, want direction line %v", metric, text, want.rises != "")
		}
	}
}

// TestExplainMetricMerged checks explain_metric says which rule a merged
// finding is attributed to (SPEC.md 8.1): tokens_est to sloc and
// duplication_pct to dup_blocks, and nothing for a metric merged into no
// other.
func TestExplainMetricMerged(t *testing.T) {
	t.Parallel()

	cs := newTestClient(t, Options{Config: defaultConfig(t)})
	for metric, into := range map[string]string{"tokens_est": "sloc", "duplication_pct": "dup_blocks", "sloc": ""} {
		text := resultText(callExplain(t, cs, metric))
		want := "Merged: when " + into + " also fires on the row in the same direction, the two are one finding attributed to " +
			into + ", whose limit names both and whose also list names " + metric + ".\n"
		if into == "" {
			if strings.Contains(text, "Merged:") {
				t.Errorf("%s: text = %q, want no merge line", metric, text)
			}
			continue
		}
		if !strings.Contains(text, want) {
			t.Errorf("%s: text = %q, want %q", metric, text, want)
		}
	}
	text := resultText(callExplain(t, cs, "duplication_pct"))
	if want := "; a change breaching both max_delta and max is one finding whose limit names both"; !strings.Contains(text, want) {
		t.Errorf("duplication_pct: text = %q, want %q", text, want)
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
