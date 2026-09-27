package mcpserver

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// callAssess calls assess_package with in and returns the result.
func callAssess(t *testing.T, cs *mcp.ClientSession, in map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: assessToolName, Arguments: in})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	return res
}

func TestAssessPackageTool(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	ws := workspace(t, fixtureDir)
	cfg := defaultConfig(t)

	tests := []struct {
		name string
		in   map[string]any
	}{
		{name: "default tokenizer", in: map[string]any{"path": "mod/hub"}},
		{name: "est", in: map[string]any{"path": "mod/hub", "tokenizer": "est"}},
		// o200k's encoding is embedded, so this runs offline.
		{name: "o200k", in: map[string]any{"path": "mod/hub", "tokenizer": "o200k"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cs := newTestClient(t, Options{Config: cfg, WorkDir: ws, Version: "test"})
			res := callAssess(t, cs, tt.in)
			text := resultText(res)
			if res.IsError {
				t.Fatalf("IsError = true, want a report; text:\n%s", text)
			}
			r := decodeReport(t, res)
			if r.PackagePath != "hub" || r.Language != "go" || r.Rebuild.Tier == "" {
				t.Errorf("report = (%q, %q, tier %q), want the go package hub with a tier",
					r.PackagePath, r.Language, r.Rebuild.Tier)
			}
			if r.Metrics.TokensEst <= 0 {
				t.Errorf("tokens_est = %d, want positive", r.Metrics.TokensEst)
			}
			if r.Passed != nil || r.Baseline != nil || r.Violations != nil {
				t.Errorf("report carries gate fields (passed %v, baseline %v, violations %v), want none",
					r.Passed, r.Baseline, r.Violations)
			}
			for _, want := range []string{"package: hub", "tier: " + string(r.Rebuild.Tier), "drivers:"} {
				if !strings.Contains(text, want) {
					t.Errorf("text lacks %q:\n%s", want, text)
				}
			}
		})
	}
}

func TestAssessPackageToolErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	ws := workspace(t, fixtureDir)
	if err := os.Mkdir(filepath.Join(ws, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	cfg := defaultConfig(t)
	opts := Options{Config: cfg, WorkDir: ws}

	tests := []struct {
		name     string
		in       map[string]any
		wantText string
	}{
		{name: "not a module", in: map[string]any{"path": "plain"}, wantText: "go.mod"},
		{name: "outside path refused", in: map[string]any{"path": outside},
			wantText: "outside the server's working directory"},
		{name: "parent path refused", in: map[string]any{"path": ".."},
			wantText: "outside the server's working directory"},
		{name: "missing path", in: map[string]any{"path": "mod/nope"}, wantText: "mod/nope"},
		{name: "unknown tokenizer", in: map[string]any{"path": "mod/hub", "tokenizer": "cl100k"},
			wantText: "unknown tokenizer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := callAssess(t, newTestClient(t, opts), tt.in)
			text := resultText(res)
			if !res.IsError {
				t.Fatalf("IsError = false, want true; text:\n%s", text)
			}
			if !strings.HasPrefix(text, assessToolName+": ") || !strings.Contains(text, tt.wantText) {
				t.Errorf("text = %q, want it to name %s and contain %q", text, assessToolName, tt.wantText)
			}
			data, err := json.Marshal(res.StructuredContent)
			if err != nil || !bytes.Contains(data, []byte(`"error"`)) {
				t.Errorf("structured content = %s, want an error object", data)
			}
		})
	}
}
