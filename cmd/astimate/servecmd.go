package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/mcpserver"
)

// runServe starts the MCP server on stdio: `serve [--config path]
// [--allow-any-path]`. The configuration is resolved once, as for the other
// commands, and shared by every tool; its warnings, such as deprecated keys
// and language overrides no extractor reports, are logged once before the
// server starts. Stdout belongs to the protocol, so
// runServe never writes to the stdout it is given; usage errors and logs go
// to stderr. It exits 0 when the client closes stdin or on SIGINT or
// SIGTERM, 2 when the configuration cannot be resolved or serving fails,
// and 1 on a usage error.
func runServe(args []string, _, stderr io.Writer) int {
	fs := flag.NewFlagSet("astimate serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "configuration file (default ./astimate.yaml, then the embedded default)")
	allowAnyPath := fs.Bool("allow-any-path", false, "let tools read paths outside the working directory")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: astimate serve [--config path] [--allow-any-path]")
		fs.PrintDefaults()
	}
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage // the flag package has printed the error and usage
	}
	if len(positional) != 0 {
		_, _ = fmt.Fprintf(stderr, "astimate: serve: takes no arguments, got %d\n", len(positional))
		fs.Usage()
		return exitUsage
	}

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, source, err := config.Resolve(*configPath)
	if err != nil {
		logger.Error("serve failed", "err", fmt.Errorf("resolving config: %w", err))
		return exitAnalysis
	}
	langWarns, err := engine.LanguageWarnings(cfg)
	if err != nil {
		logger.Error("serve failed", "err", fmt.Errorf("checking config languages: %w", err))
		return exitAnalysis
	}
	for _, w := range slices.Concat(cfg.Warnings, langWarns) {
		logger.Warn("config", "source", source, "warning", w)
	}
	wd, err := os.Getwd()
	if err != nil {
		logger.Error("serve failed", "err", fmt.Errorf("reading working directory: %w", err))
		return exitAnalysis
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	version := astimateVersion()
	logger.Info("mcp server starting", "version", version, "config", source, "workdir", wd, "allow_any_path", *allowAnyPath)
	err = mcpserver.Serve(ctx, mcpserver.Options{
		Config:       cfg,
		AllowAnyPath: *allowAnyPath,
		WorkDir:      wd,
		Version:      version,
		Logger:       logger,
	})
	if err != nil {
		logger.Error("serve failed", "err", err)
		return exitAnalysis
	}
	logger.Info("mcp server stopped")
	return exitOK
}
