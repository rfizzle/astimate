package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/report"
)

// assessToolName is the name the assess tool is registered under (SPEC.md
// 10.1).
const assessToolName = "assess_package"

// assessToolDescription tells the agent what assess_package reports and
// when it helps.
const assessToolDescription = "Report the rebuild estimate for one Go package: agent passes, rebuild tokens, " +
	"human days and tier, the drivers behind the estimate, suggestions, every metric, and details " +
	"locating its duplicate blocks, untested exports and globals. Call it before " +
	"deciding how to approach a change to a package, to see how large and risky it is and what makes it so. " +
	"It compares against no baseline; use check_package to gate a change."

// AssessInput is the input of the assess_package tool.
type AssessInput struct {
	// Path is the package directory, relative to the server's working
	// directory or absolute.
	Path string `json:"path" jsonschema:"Directory of the package to assess, relative to the server's working directory or absolute. Must be inside the working directory unless the server runs with --allow-any-path."`
	// Tokenizer selects the token counter: est (default) or o200k.
	Tokenizer string `json:"tokenizer,omitempty" jsonschema:"Token counter: est (the default, a character-based estimate) or o200k (an exact count with the o200k encoding, slower)."`
	// Coverage measures coverage_pct (engine.AssessOptions.Coverage).
	Coverage bool `json:"coverage,omitempty" jsonschema:"Run the package's tests with go test -cover to report coverage_pct and scale the estimate's unspecified term by it. Slower. Defaults to false."`
}

// addAssessTool registers assess_package on srv, backed by s.
func addAssessTool(srv *mcp.Server, s *session) {
	mcp.AddTool(srv, withOutputSchema[report.Report](&mcp.Tool{
		Name:        assessToolName,
		Title:       "Assess a package's rebuild estimate",
		Description: assessToolDescription,
	}, s.opts.logger()), s.assessPackage)
}

// assessPackage handles an assess_package call. An input, resolution or
// extraction failure is a result with isError set. Either way the result
// carries text content and structured content: the package's SPEC.md 10.2
// report, or {"error": ...}.
func (s *session) assessPackage(ctx context.Context, _ *mcp.CallToolRequest, in AssessInput) (*mcp.CallToolResult, any, error) {
	r, text, err := s.runAssess(ctx, in)
	if err != nil {
		msg := assessToolName + ": " + err.Error()
		s.opts.logger().Warn("tool call failed", "tool", assessToolName, "path", in.Path, "err", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: msg}},
			IsError: true,
		}, checkError{Error: msg}, nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, r, nil
}

// runAssess assesses the package in.Path and returns its report and table
// rendering. The default tokenizer reuses the session's cached Target for
// the module root. Another tokenizer needs an extractor of its own, so it
// gets an uncached Target built for this call only: the cache stays one
// Target per root, and the rarer exact count pays for its own load.
func (s *session) runAssess(ctx context.Context, in AssessInput) (*report.Report, string, error) {
	tokenizer := in.Tokenizer
	if tokenizer == "" {
		tokenizer = engine.TokenizerEst
	}
	if !engine.ValidTokenizer(tokenizer) {
		return nil, "", fmt.Errorf("%w %q: want %s or %s", engine.ErrUnknownTokenizer, tokenizer,
			engine.TokenizerEst, engine.TokenizerO200k)
	}
	dir, err := s.opts.resolvePath(in.Path)
	if err != nil {
		return nil, "", err
	}

	var t *engine.Target
	if tokenizer == engine.TokenizerEst {
		var rc *rootCache
		if t, rc, err = s.loadTarget(dir); err != nil {
			return nil, "", err
		}
		defer rc.mu.Unlock()
	} else {
		t, err = engine.LoadTarget(dir, engine.TargetOptions{
			Config:    s.opts.Config,
			Tokenizer: tokenizer,
			Version:   s.opts.Version,
			Logger:    s.opts.logger(),
		})
		if err != nil {
			return nil, "", err
		}
	}
	r, err := engine.Assess(ctx, t, engine.AssessOptions{Coverage: engine.CoverageOptions{Enabled: in.Coverage}})
	if err != nil {
		return nil, "", err
	}
	var b strings.Builder
	if err := report.WriteTable(&b, r); err != nil {
		return nil, "", err
	}
	return r, b.String(), nil
}
