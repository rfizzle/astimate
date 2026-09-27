package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/config"
)

func TestDiscovery(t *testing.T) {
	t.Parallel()

	cfg, _, err := config.Resolve("")
	if err != nil {
		t.Fatalf("resolving default config: %v", err)
	}
	cs := newTestClient(t, Options{Config: cfg, Version: "v1.2.3"})

	init := cs.InitializeResult()
	if init == nil || init.ServerInfo == nil {
		t.Fatalf("InitializeResult = %+v, want server info", init)
	}
	if got := init.ServerInfo.Name; got != "astimate" {
		t.Errorf("server name = %q, want %q", got, "astimate")
	}
	if got := init.ServerInfo.Version; got != "v1.2.3" {
		t.Errorf("server version = %q, want %q", got, "v1.2.3")
	}
	if init.Capabilities == nil || init.Capabilities.Tools == nil {
		t.Errorf("capabilities = %+v, want tools advertised", init.Capabilities)
	}

	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	tools := make(map[string]*mcp.Tool, len(res.Tools))
	for _, tool := range res.Tools {
		tools[tool.Name] = tool
	}
	tests := []struct {
		name     string
		props    []string
		whenText string
	}{
		{name: checkToolName, props: []string{`"path"`, `"base"`, `"baseline_file"`, `"required":["path"]`},
			whenText: "before declaring the work done"},
		{name: assessToolName, props: []string{`"path"`, `"tokenizer"`, `"required":["path"]`},
			whenText: "before deciding how to approach a change"},
		{name: rankToolName, props: []string{`"module_root"`, `"top"`, `"sort"`},
			whenText: "before choosing what to touch"},
	}
	if len(tools) != len(tests) {
		t.Fatalf("ListTools returned %d tools, want %d", len(tools), len(tests))
	}
	for _, tt := range tests {
		tool, ok := tools[tt.name]
		if !ok {
			t.Errorf("ListTools lacks %s", tt.name)
			continue
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		for _, prop := range tt.props {
			if !strings.Contains(string(schema), prop) {
				t.Errorf("%s input schema %s lacks %s", tt.name, schema, prop)
			}
		}
		if !strings.Contains(tool.Description, tt.whenText) {
			t.Errorf("%s description = %q, want it to say when to call the tool", tt.name, tool.Description)
		}
	}
}

func TestServeReturnsNilOnCancel(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	serverT, clientT := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, New(Options{Version: "test"}), serverT, logger) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "astimate-test", Version: "test"}, nil)
	cs, err := client.Connect(t.Context(), clientT, nil)
	if err != nil {
		t.Fatalf("connecting test client: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve after cancel = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve did not return within one second of cancellation")
	}
	if !strings.Contains(logs.String(), "mcp session cancelled") {
		t.Errorf("logs = %q, want the cancellation logged", logs.String())
	}
}
