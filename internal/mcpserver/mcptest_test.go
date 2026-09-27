package mcpserver

import (
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newTestClient starts the server for o on one end of an in-memory
// transport pair and returns a client session connected to the other end.
// Cleanup closes the client and waits for the server to return nil. Tool
// tests use it to call tools without a subprocess.
func newTestClient(t *testing.T, o Options) *mcp.ClientSession {
	t.Helper()

	serverT, clientT := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- serve(t.Context(), New(o), serverT, o.logger()) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "astimate-test", Version: "test"}, nil)
	cs, err := client.Connect(t.Context(), clientT, nil)
	if err != nil {
		t.Fatalf("connecting test client: %v", err)
	}
	t.Cleanup(func() {
		if err := cs.Close(); err != nil && !errors.Is(err, mcp.ErrConnectionClosed) {
			t.Errorf("closing test client: %v", err)
		}
		if err := <-done; err != nil {
			t.Errorf("server returned %v, want nil after the client closed", err)
		}
	})
	return cs
}
