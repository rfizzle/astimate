package main

// Build metadata injected at link time, for example:
//
//	go build -ldflags "-X main.buildVersion=v0.1.0 -X main.buildCommit=abc123 -X main.buildDate=2026-01-01T00:00:00Z"
//
// Empty values fall back to runtime/debug build info; see resolveBuildMeta.
var buildVersion string //nolint:gochecknoglobals // set by -ldflags at build time

var buildCommit string //nolint:gochecknoglobals // set by -ldflags at build time

var buildDate string //nolint:gochecknoglobals // set by -ldflags at build time
