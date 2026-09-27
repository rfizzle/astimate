package mcpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/report"
)

// callRank calls rank_packages with in and returns the result.
func callRank(t *testing.T, cs *mcp.ClientSession, in map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: rankToolName, Arguments: in})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	return res
}

// decodeRank decodes the structured content of res as a ranking result,
// rejecting unknown fields.
func decodeRank(t *testing.T, res *mcp.CallToolResult) rankResult {
	t.Helper()
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("encoding structured content: %v", err)
	}
	var r rankResult
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("decoding structured content as a ranking: %v\n%s", err, data)
	}
	if r.Rows == nil || r.Failed == nil {
		t.Errorf("structured content = %s, want rows and failed as arrays", data)
	}
	return r
}

func TestRankPackagesTool(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	cs := newTestClient(t, Options{Config: defaultConfig(t), WorkDir: fixtureDir})

	tests := []struct {
		name      string
		in        map[string]any
		wantRows  int
		wantFirst string
		sorted    func(a, b report.Row) bool
	}{
		{name: "default sort", in: map[string]any{}, wantRows: 7,
			sorted: func(a, b report.Row) bool { return a.AgentPasses >= b.AgentPasses }},
		{name: "duplication", in: map[string]any{"sort": "duplication"}, wantRows: 7, wantFirst: "dupes",
			sorted: func(a, b report.Row) bool { return a.DuplicationPct >= b.DuplicationPct }},
		{name: "top two", in: map[string]any{"top": 2, "module_root": "."}, wantRows: 2,
			sorted: func(a, b report.Row) bool { return a.AgentPasses >= b.AgentPasses }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := callRank(t, cs, tt.in)
			text := resultText(res)
			if res.IsError {
				t.Fatalf("IsError = true; text:\n%s", text)
			}
			r := decodeRank(t, res)
			if len(r.Rows) != tt.wantRows {
				t.Fatalf("got %d rows, want %d; text:\n%s", len(r.Rows), tt.wantRows, text)
			}
			if len(r.Failed) != 0 {
				t.Errorf("failed = %v, want none", r.Failed)
			}
			for i := 1; i < len(r.Rows); i++ {
				if !tt.sorted(r.Rows[i-1], r.Rows[i]) {
					t.Errorf("rows %d and %d out of order: %+v, %+v", i-1, i, r.Rows[i-1], r.Rows[i])
				}
			}
			if tt.wantFirst != "" && r.Rows[0].Path != tt.wantFirst {
				t.Errorf("first row = %q, want %q", r.Rows[0].Path, tt.wantFirst)
			}
			if !strings.HasPrefix(text, "PATH") || strings.Count(text, "\n") != tt.wantRows+1 {
				t.Errorf("text = %q, want a header and %d table lines", text, tt.wantRows)
			}
			if strings.Contains(text, "skipped") {
				t.Errorf("text = %q, want no skipped list", text)
			}
		})
	}
}

func TestRankPackagesToolErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	cfg := defaultConfig(t)
	tests := []struct {
		name     string
		in       map[string]any
		wantText string
	}{
		{name: "unknown sort", in: map[string]any{"sort": "size"},
			wantText: "want one of passes, days, fan_in, tokens, duplication"},
		{name: "negative top", in: map[string]any{"top": -1}, wantText: "top is -1"},
		{name: "outside work dir", in: map[string]any{"module_root": ".."}, wantText: "outside the server's working directory"},
		{name: "missing root", in: map[string]any{"module_root": "nope"}, wantText: "module_root"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := callRank(t, newTestClient(t, Options{Config: cfg, WorkDir: fixtureDir}), tt.in)
			text := resultText(res)
			if !res.IsError {
				t.Fatalf("IsError = false, want true; text:\n%s", text)
			}
			if !strings.Contains(text, tt.wantText) {
				t.Errorf("text = %q, want it to contain %q", text, tt.wantText)
			}
			data, err := json.Marshal(res.StructuredContent)
			if err != nil || !bytes.Contains(data, []byte(`"error"`)) {
				t.Errorf("structured content = %s, want an error object", data)
			}
		})
	}
}

// TestRankOutputPartialFailure covers the rendering of skipped packages.
// The fixtures have no module where only some packages fail to extract;
// engine.Rank's partial-failure path is covered by internal/engine's tests.
func TestRankOutputPartialFailure(t *testing.T) {
	t.Parallel()

	rows := []report.Row{{Path: "a", AgentPasses: 1.5, Tier: "ONE_PASS"}}
	failed := []error{
		&engine.PackageError{Path: "broken", Err: errors.New("type-checking: undefined: x")},
		errors.New("bare failure"),
	}
	text, res, err := rankOutput(rows, failed)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PATH", "\nskipped 2 packages:\n", "  broken: type-checking: undefined: x\n", "  bare failure\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("text = %q, want it to contain %q", text, want)
		}
	}
	want := []rankFailure{{Path: "broken", Error: "type-checking: undefined: x"}, {Error: "bare failure"}}
	if len(res.Rows) != 1 || len(res.Failed) != len(want) || res.Failed[0] != want[0] || res.Failed[1] != want[1] {
		t.Errorf("structured = %+v, want one row and failed %+v", res, want)
	}

	text, res, err = rankOutput(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "skipped") || res.Rows == nil || res.Failed == nil {
		t.Errorf("empty ranking = (%q, %+v), want no skipped list and empty arrays", text, res)
	}
}
