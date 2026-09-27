package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/report"
)

// checkToolName is the name the check tool is registered under (SPEC.md
// 10.1).
const checkToolName = "check_package"

// checkToolDescription tells the agent when to call check_package and how
// to read its result.
const checkToolDescription = "Run the astimate quality gate on one Go package you changed and report " +
	"whether it got worse than its baseline. Call it on each package you changed before declaring " +
	"the work done. A result with passed: false lists the violations to fix, each with a suggestion; " +
	"fix them and call it again until it passes. Warnings do not fail the gate. Do not call it on " +
	"packages you did not touch. The baseline is the merge-base of HEAD and the default branch " +
	"unless base or baseline_file says otherwise."

// CheckInput is the input of the check_package tool.
type CheckInput struct {
	// Path is the package directory, relative to the server's working
	// directory or absolute.
	Path string `json:"path" jsonschema:"Directory of the package to check, relative to the server's working directory or absolute. Must be inside the working directory unless the server runs with --allow-any-path."`
	// Base is the git ref whose merge-base with HEAD is the baseline.
	Base string `json:"base,omitempty" jsonschema:"Git ref to compare against: the baseline is the merge-base of HEAD and this ref. Defaults to origin/master, then master, origin/main, main. Do not combine with baseline_file."`
	// BaselineFile is a baseline file written by `astimate baseline write`.
	BaselineFile string `json:"baseline_file,omitempty" jsonschema:"Baseline file written by 'astimate baseline write' to compare against instead of a git ref, relative to the server's working directory or absolute."`
}

// checkError is the structured content of a failed check_package call.
type checkError struct {
	Error string `json:"error"`
}

// addCheckTool registers check_package on srv, backed by s.
func addCheckTool(srv *mcp.Server, s *session) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        checkToolName,
		Title:       "Check a package against its baseline",
		Description: checkToolDescription,
	}, s.checkPackage)
}

// checkPackage handles a check_package call. A gate failure is a normal
// result with passed false; an input, resolution or extraction failure is
// a result with isError set. Either way the result carries text content and
// structured content: the package's SPEC.md 10.2 report, or {"error": ...}.
func (s *session) checkPackage(ctx context.Context, _ *mcp.CallToolRequest, in CheckInput) (*mcp.CallToolResult, any, error) {
	r, text, err := s.runCheck(ctx, in)
	if err != nil {
		msg := checkToolName + ": " + err.Error()
		if errors.Is(err, engine.ErrNoBaseline) {
			msg += "; pass base (a git ref) or baseline_file"
		}
		s.opts.logger().Warn("tool call failed", "tool", checkToolName, "path", in.Path, "err", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: msg}},
			IsError: true,
		}, checkError{Error: msg}, nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, r, nil
}

// runCheck gates the package in.Path and returns its report and the text
// rendering for the agent.
func (s *session) runCheck(ctx context.Context, in CheckInput) (*report.Report, string, error) {
	if in.Base != "" && in.BaselineFile != "" {
		return nil, "", errors.New("base and baseline_file are mutually exclusive; pass one")
	}
	dir, err := s.opts.resolvePath(in.Path)
	if err != nil {
		return nil, "", err
	}
	baselineFile := ""
	if in.BaselineFile != "" {
		if baselineFile, err = s.opts.resolvePath(in.BaselineFile); err != nil {
			return nil, "", fmt.Errorf("baseline_file: %w", err)
		}
	}

	t, rc, err := s.loadTarget(dir)
	if err != nil {
		return nil, "", err
	}
	defer rc.mu.Unlock()
	c, failed, err := engine.Check(ctx, t, engine.CheckOptions{
		Base:         in.Base,
		BaselineFile: baselineFile,
		Packages:     []string{t.ImportPath},
		Baselines:    rc.baselines,
	})
	if err != nil {
		return nil, "", err
	}
	if len(failed) > 0 {
		return nil, "", errors.Join(failed...)
	}
	if len(c.Packages) != 1 {
		return nil, "", fmt.Errorf("checking %s: got %d reports, want 1", t.Dir, len(c.Packages))
	}
	text, err := checkText(c)
	if err != nil {
		return nil, "", err
	}
	return &c.Packages[0].Report, text, nil
}

// checkText renders the one-package check c for the agent: a verdict line
// saying what to do next, then the check's text report.
func checkText(c *report.Check) (string, error) {
	r := &c.Packages[0].Report
	var b strings.Builder
	if c.Failed() {
		b.WriteString("FAILED: " + r.PackagePath + " has " + strconv.Itoa(len(r.Violations)) +
			" violation(s). Fix each one below, then call " + checkToolName + " again.\n")
	} else {
		b.WriteString("PASSED: " + r.PackagePath + " is no worse than its baseline.\n")
	}
	if err := report.WriteCheckText(&b, c); err != nil {
		return "", err
	}
	return b.String(), nil
}
