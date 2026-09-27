package mcpserver

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// resolveSchema resolves the JSON schema s, as listed by tools/list.
func resolveSchema(t *testing.T, s any) *jsonschema.Resolved {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshaling schema: %v", err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("decoding schema %s: %v", data, err)
	}
	r, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolving schema %s: %v", data, err)
	}
	return r
}

// TestToolOutputSchemas lists the tools and, for each, checks that its
// output schema is a oneOf of the result and {"error": string}, and that a
// successful call's and a failed call's structured content each validate
// against the listed schema and match exactly the branch of their kind.
func TestToolOutputSchemas(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	ws := workspace(t, degradedDir)
	cs := newTestClient(t, Options{Config: defaultConfig(t), WorkDir: ws})

	calls := map[string]struct{ ok, failed map[string]any }{
		checkToolName: {
			ok:     map[string]any{"path": "mod/tested", "baseline_file": "baseline.json"},
			failed: map[string]any{"path": "missing", "baseline_file": "baseline.json"},
		},
		assessToolName: {
			ok:     map[string]any{"path": "mod/tested"},
			failed: map[string]any{"path": "missing"},
		},
		rankToolName: {
			ok:     map[string]any{"module_root": "mod"},
			failed: map[string]any{"module_root": "missing"},
		},
		explainToolName: {
			ok:     map[string]any{"metric": "dup_blocks"},
			failed: map[string]any{"metric": "missing"},
		},
	}

	list, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list.Tools) != len(calls) {
		t.Errorf("tools/list returned %d tools, want %d", len(list.Tools), len(calls))
	}
	for _, tool := range list.Tools {
		t.Run(tool.Name, func(t *testing.T) {
			in, ok := calls[tool.Name]
			if !ok {
				t.Fatalf("no calls for tool %q", tool.Name)
			}
			if tool.OutputSchema == nil {
				t.Fatal("tool has no output schema")
			}
			schema := resolveSchema(t, tool.OutputSchema)
			var shape struct {
				Type  string `json:"type"`
				OneOf []any  `json:"oneOf"`
			}
			data, err := json.Marshal(tool.OutputSchema)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &shape); err != nil || shape.Type != "object" || len(shape.OneOf) != 2 {
				t.Fatalf("output schema = %s, want an object with a two-branch oneOf", data)
			}
			branches := []*jsonschema.Resolved{resolveSchema(t, shape.OneOf[0]), resolveSchema(t, shape.OneOf[1])}

			for _, kind := range []struct {
				name    string
				args    map[string]any
				isError bool
				branch  int
			}{
				{name: "success", args: in.ok, branch: 0},
				{name: "error", args: in.failed, isError: true, branch: 1},
			} {
				res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool.Name, Arguments: kind.args})
				if err != nil {
					t.Fatalf("%s call: %v", kind.name, err)
				}
				if res.IsError != kind.isError {
					t.Fatalf("%s call isError = %v, want %v: %s", kind.name, res.IsError, kind.isError, resultText(res))
				}
				data, err := json.Marshal(res.StructuredContent)
				if err != nil {
					t.Fatal(err)
				}
				var content any
				if err := json.Unmarshal(data, &content); err != nil {
					t.Fatalf("%s structured content %s: %v", kind.name, data, err)
				}
				if err := schema.Validate(content); err != nil {
					t.Errorf("%s structured content does not validate: %v\n%s", kind.name, err, data)
				}
				for i, b := range branches {
					err := b.Validate(content)
					if want := i == kind.branch; (err == nil) != want {
						t.Errorf("%s structured content matches branch %d = %v, want %v (err %v)", kind.name, i, err == nil, want, err)
					}
				}
			}
		})
	}
}
