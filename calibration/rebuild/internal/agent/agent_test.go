package agent

import (
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/stub"
)

func TestParseAgentOutput(t *testing.T) {
	stream := `{"type":"system","subtype":"init","session_id":"abc","tools":["Read"]}
{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"hi"},{"type":"tool_use","id":"t1","name":"Read"}]}}
{"type":"assistant","message":{"id":"m1","content":[{"type":"tool_use","id":"t2","name":"Edit"}]}}
{"type":"user","message":{"content":"a string, not blocks"}}
{"type":"assistant","message":{"id":"m2","content":[{"type":"tool_use","id":"t2","name":"Edit"}]}}
not json at all
{"type":"result","subtype":"success","is_error":false,"duration_ms":5000,"duration_api_ms":4000,"num_turns":12,` +
		`"session_id":"abc","total_cost_usd":0.25,"future_field":{"x":1},` +
		`"usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":30,"cache_read_input_tokens":40},` +
		`"modelUsage":{"model-b":{"inputTokens":1,"outputTokens":2,"cacheReadInputTokens":3,"cacheCreationInputTokens":4},` +
		`"model-a":{"inputTokens":10,"outputTokens":20,"cacheReadInputTokens":30,"cacheCreationInputTokens":40}}}
`
	tests := []struct {
		name    string
		out     string
		turnCap int
		check   func(t *testing.T, m Measured)
	}{
		{"stream-json", stream, 100, func(t *testing.T, m Measured) {
			if *m.InputTokens != 11 || *m.OutputTokens != 22 || *m.CacheReadTokens != 33 || *m.CacheWriteTokens != 44 {
				t.Errorf("tokens %d %d %d %d, want the modelUsage sums", *m.InputTokens, *m.OutputTokens,
					*m.CacheReadTokens, *m.CacheWriteTokens)
			}
			if *m.TokenSource != "modelUsage" || *m.ToolCalls != 2 || *m.Turns != 12 || *m.CostUSD != 0.25 ||
				*m.SessionID != "abc" || *m.DurationMS != 5000 || *m.APIDurationMS != 4000 || *m.TurnCapHit {
				t.Errorf("unexpected %+v", m)
			}
			if !slices.Equal(m.Models, []string{"model-a", "model-b"}) || len(m.Missing) != 0 {
				t.Errorf("models %v missing %v", m.Models, m.Missing)
			}
		}},
		{"json result pretty printed", `{
  "type": "result", "subtype": "error_max_turns", "is_error": true, "num_turns": 101,
  "usage": {"input_tokens": 5, "output_tokens": 6}
}`, 100, func(t *testing.T, m Measured) {
			if *m.TokenSource != "usage" || *m.InputTokens != 5 || !*m.TurnCapHit || !*m.IsError {
				t.Errorf("unexpected %+v", m)
			}
			want := []string{"cache_read_tokens", "cache_write_tokens", "cost_usd", "tool_calls", "duration_ms",
				"api_duration_ms", "session_id", "models"}
			if !slices.Equal(m.Missing, want) {
				t.Errorf("missing %v, want %v", m.Missing, want)
			}
		}},
		{"turns reach the cap", `{"type":"result","num_turns":100}`, 100, func(t *testing.T, m Measured) {
			if !*m.TurnCapHit {
				t.Error("turn cap not detected")
			}
		}},
		{"no output", "", 100, func(t *testing.T, m Measured) {
			if m.InputTokens != nil || m.TurnCapHit != nil || m.ToolCalls != nil || len(m.Missing) != 15 {
				t.Errorf("want everything null, got %+v", m)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, ParseAgentOutput([]byte(tt.out), tt.turnCap))
		})
	}
}

func TestAgentTemplate(t *testing.T) {
	if err := CheckTemplate(DefaultTemplate); err != nil {
		t.Fatal(err)
	}
	if err := CheckTemplate(`x {nope}`); err == nil {
		t.Error("unknown placeholder accepted")
	}
	if err := CheckTemplate(`echo "${HOME}" {dir} {X}`); err != nil {
		t.Errorf("shell expansion or an upper-case brace taken for a placeholder: %v", err)
	}
	got := RenderTemplate(`run {dir} {prompt_file} "${dir}" {turn_cap}`, map[string]string{
		"dir": "a b", "prompt_file": "/tmp/it's", "turn_cap": "100",
	})
	want := `run 'a b' '/tmp/it'\''s' "${dir}" 100`
	if got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", "''"},
		{"plain", "plain"},
		{"a b", "'a b'"},
		{"it's", `'it'\''s'`},
	}
	for _, tt := range tests {
		if got := ShellQuote(tt.in); got != tt.want {
			t.Errorf("ShellQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPlaceholders(t *testing.T) {
	want := []string{"root", "dir", "package", "prompt_file", "turn_cap", "model"}
	if got := Placeholders(); !slices.Equal(got, want) {
		t.Fatalf("Placeholders() = %v, want %v", got, want)
	}
}

// TestBuildPrompt checks the fixed prompt for a tested and an untested
// package.
func TestBuildPrompt(t *testing.T) {
	tested := &definition.Experiment{Package: "example.com/m/lib", Dir: "lib", HasTests: true,
		Oracle: definition.Oracle{Test: []string{"./lib"}, Build: []string{"./..."}}}
	p := BuildPrompt(tested, "/root", []string{"CGO_ENABLED=0"}, stub.Summary{})
	if !strings.Contains(p, "example.com/m/lib") || !strings.Contains(p, "/root") ||
		!strings.Contains(p, "go test ./lib") || !strings.Contains(p, "Do not edit test files") ||
		!strings.Contains(p, "CGO_ENABLED=0 go test ./lib && go build ./...") {
		t.Fatalf("BuildPrompt (tested) =\n%s", p)
	}

	untested := &definition.Experiment{Package: "example.com/m/lib", Dir: "lib", HasTests: false,
		Oracle: definition.Oracle{Test: []string{"./use1", "./use2"}, Build: []string{"./..."}}}
	p = BuildPrompt(untested, "/root", []string{"CGO_ENABLED=0"}, stub.Summary{})
	if !strings.Contains(p, "no tests of its own") || !strings.Contains(p, "go test ./use1 ./use2") {
		t.Fatalf("BuildPrompt (untested) =\n%s", p)
	}
	if strings.Contains(p, "package initialization") {
		t.Fatalf("BuildPrompt mentions removed initialization for a stub that removed none:\n%s", p)
	}
	if p := BuildPrompt(tested, "/root", nil, stub.Summary{Inits: 1}); !strings.Contains(p, "package initialization no longer "+
		"calls the package's own functions or methods. Package-level variables whose initializers did") {
		t.Fatalf("BuildPrompt (removed initialization) =\n%s", p)
	}
}

// TestBuildPromptTree checks the prompt of a tree: it names the tree and
// every package, runs the tree's tests, and rules out other packages.
func TestBuildPromptTree(t *testing.T) {
	e := &definition.Experiment{Package: "example.com/m/a", Dir: "a", HasTests: true,
		Oracle:  definition.Oracle{Test: []string{"./a/..."}, Build: []string{"./..."}},
		Members: []definition.Member{{Package: "example.com/m/a", Dir: "a"}, {Package: "example.com/m/a/b", Dir: "a/b"}}}
	p := BuildPrompt(e, "/root", []string{"CGO_ENABLED=0"}, stub.Summary{})
	for _, want := range []string{"directory tree ./a", "- example.com/m/a (directory ./a)", "- example.com/m/a/b (directory ./a/b)",
		"`go test ./a/...` passes", "Do not touch packages outside ./a", "CGO_ENABLED=0 go test ./a/... && go build ./..."} {
		if !strings.Contains(p, want) {
			t.Fatalf("BuildPrompt (tree) lacks %q:\n%s", want, p)
		}
	}
	e.HasTests, e.Oracle.Test = false, []string{"./use"}
	if p := BuildPrompt(e, "/root", nil, stub.Summary{}); !strings.Contains(p, "None of the packages has tests") ||
		!strings.Contains(p, "go test ./use") {
		t.Fatalf("BuildPrompt (untested tree) =\n%s", p)
	}
}
