package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/config"
)

// serverName is the implementation name the server reports on initialize.
const serverName = "astimate"

// Options configure the MCP server. One Options value is shared by every
// tool, so a tool reads the configuration, path policy and logger from here
// rather than from flags of its own.
type Options struct {
	// Config is the resolved configuration every tool evaluates against.
	Config *config.Config
	// AllowAnyPath lets tools read paths outside WorkDir.
	AllowAnyPath bool
	// WorkDir is the directory relative tool paths resolve against and,
	// unless AllowAnyPath is set, the directory tools are confined to.
	WorkDir string
	// Version is the astimate build version reported on initialize.
	Version string
	// Logger receives all server logging. It must not write to stdout,
	// which carries protocol frames only. Nil discards logs.
	Logger *slog.Logger
}

// New returns the astimate MCP server for o. Each tool is registered by its
// own file in this package (tool_check.go, tool_assess.go, tool_rank.go,
// tool_explain.go) through an AddTool call made from here. The tools share
// one session, which caches module loads and baselines for the server's
// lifetime.
func New(o Options) *mcp.Server {
	srv := mcp.NewServer(
		&mcp.Implementation{Name: serverName, Version: o.Version},
		&mcp.ServerOptions{
			Logger: o.logger(),
			// Advertise tools without listChanged: the tool set is fixed at
			// startup. Leaving Capabilities nil would advertise logging,
			// which this server does not implement.
			Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
		},
	)
	s := newSession(o)
	addAssessTool(srv, s)
	addCheckTool(srv, s)
	addRankTool(srv, s)
	addExplainTool(srv, s)
	return srv
}

// Serve runs the server on stdin and stdout until the client closes stdin or
// ctx is cancelled, and returns nil in both cases.
func Serve(ctx context.Context, o Options) error {
	return serve(ctx, New(o), &mcp.StdioTransport{}, o.logger())
}

// serve connects s over t and waits for the session to end. It returns nil
// when the peer closes the connection or ctx is cancelled, and the session
// error otherwise.
func serve(ctx context.Context, s *mcp.Server, t mcp.Transport, logger *slog.Logger) error {
	ss, err := s.Connect(ctx, t, nil)
	if err != nil {
		return fmt.Errorf("connecting MCP session: %w", err)
	}
	logger.Info("mcp session started")
	done := make(chan error, 1)
	go func() { done <- ss.Wait() }()

	select {
	case <-ctx.Done():
		_ = ss.Close() // shutting down; a close error changes nothing
		<-done
		logger.Info("mcp session cancelled")
		return nil
	case err := <-done:
		if err != nil && !isClosed(err) {
			return fmt.Errorf("serving MCP session: %w", err)
		}
		logger.Info("mcp session ended")
		return nil
	}
}

// isClosed reports whether err only says the peer went away.
func isClosed(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, mcp.ErrConnectionClosed)
}

// logger returns o.Logger, or a logger that discards everything.
func (o *Options) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.New(slog.DiscardHandler)
}
