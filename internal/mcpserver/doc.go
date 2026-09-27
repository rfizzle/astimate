// Package mcpserver exposes astimate's gate, assess and rank operations as
// Model Context Protocol tools, built on the official Go SDK
// (github.com/modelcontextprotocol/go-sdk).
//
// The server speaks newline-delimited JSON-RPC on stdio. Stdout carries
// protocol frames only; all logging goes to stderr through the slog.Logger
// in Options.
package mcpserver
