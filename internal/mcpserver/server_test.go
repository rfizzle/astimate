package mcpserver

import (
	"bytes"
	"context"
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
	if len(res.Tools) != 0 {
		t.Errorf("ListTools returned %d tools, want 0", len(res.Tools))
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
