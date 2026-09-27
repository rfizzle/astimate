package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
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
	"the work done. A result with passed: false lists the violations to fix, each with a suggestion " +
	"and, where known, the location (file and line) that caused it; " +
	"fix them and call it again until it passes. Violations of module-wide rules, such as code " +
	"newly copied between this package and another, are in the module block and fail the check " +
	"too; their suggestion names the packages sharing each copy. Copies between two other " +
	"packages do not fail it. Warnings do not fail the gate. Do not call it on " +
	"packages you did not touch. The baseline is the merge-base of HEAD and the default branch " +
	"unless base or baseline_file says otherwise. Before a partial commit, pass staged: true to " +
	"judge what the git index holds rather than the working tree."

// CheckInput is the input of the check_package tool.
type CheckInput struct {
	// Path is the package directory, relative to the server's working
	// directory or absolute.
	Path string `json:"path" jsonschema:"Directory of the package to check, relative to the server's working directory or absolute. Must be inside the working directory unless the server runs with --allow-any-path."`
	// Base is the git ref whose merge-base with HEAD is the baseline.
	Base string `json:"base,omitempty" jsonschema:"Git ref to compare against: the baseline is the merge-base of HEAD and this ref. Defaults to origin/master, then master, origin/main, main. Do not combine with baseline_file."`
	// BaselineFile is a baseline file written by `astimate baseline write`.
	BaselineFile string `json:"baseline_file,omitempty" jsonschema:"Baseline file written by 'astimate baseline write' to compare against instead of a git ref, relative to the server's working directory or absolute."`
	// Staged checks the tree the git index holds instead of the working
	// tree (engine.CheckOptions.Staged). The server is not a git hook, so
	// it reads the repository's own index.
	Staged bool `json:"staged,omitempty" jsonschema:"Check what the git index holds instead of the working tree, so a partial commit is judged on what it commits and unstaged changes are ignored. Needs a git repository. Defaults to false."`
}

// CheckResult is the structured content of a successful check_package
// call: the checked package's SPEC.md 10.2 report, with the module row's
// report beside it.
type CheckResult struct {
	// Report is the checked package's report. Its passed is false when the
	// package or the module row has a violation, so it is the verdict of
	// the whole call.
	report.Report
	// Module is the module row's report (SPEC.md 8.1), package path
	// "<module>", with its own violations, warnings and passed; absent when
	// the extractor has no module row. A cross-package copy made in the
	// checked package is a violation here, not in the package's report; a
	// copy between two other packages is counted in its metrics but is not
	// a violation (engine.Check with CheckOptions.Packages).
	Module *report.Report `json:"module,omitempty"`
}

// checkError is the structured content of a failed call to any tool.
type checkError struct {
	Error string `json:"error"`
}

// outputSchema returns the output schema of a tool whose structured content
// is a T on success and a checkError on failure: a oneOf of the two schemas
// inferred from the Go types. Inferred struct schemas forbid undeclared
// properties, so the error branch rejects a report and the success branch
// rejects {"error": ...}, and every result matches exactly one branch. The
// SDK validates each result's structured content against it before sending.
func outputSchema[T any]() (*jsonschema.Schema, error) {
	ok, err := jsonschema.For[T](nil)
	if err != nil {
		return nil, fmt.Errorf("inferring the result schema: %w", err)
	}
	failed, err := jsonschema.For[checkError](nil)
	if err != nil {
		return nil, fmt.Errorf("inferring the error schema: %w", err)
	}
	return &jsonschema.Schema{Type: "object", OneOf: []*jsonschema.Schema{ok, failed}}, nil
}

// withOutputSchema sets t's output schema to outputSchema[T] and returns t.
// Inference only fails on a result type it cannot describe, a programming
// error the tool listing tests catch; the tool is then registered without
// an output schema and the failure is logged.
func withOutputSchema[T any](t *mcp.Tool, logger *slog.Logger) *mcp.Tool {
	schema, err := outputSchema[T]()
	if err != nil {
		logger.Error("tool registered without an output schema", "tool", t.Name, "err", err)
		return t
	}
	t.OutputSchema = schema
	return t
}

// addCheckTool registers check_package on srv, backed by s.
func addCheckTool(srv *mcp.Server, s *session) {
	mcp.AddTool(srv, withOutputSchema[CheckResult](&mcp.Tool{
		Name:        checkToolName,
		Title:       "Check a package against its baseline",
		Description: checkToolDescription,
	}, s.opts.logger()), s.checkPackage)
}

// checkPackage handles a check_package call. A gate failure is a normal
// result with passed false; an input, resolution or extraction failure is
// a result with isError set. Either way the result carries text content and
// structured content: a CheckResult, or {"error": ...}.
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

// runCheck gates the package in.Path and the module row and returns the
// result and the text rendering for the agent.
func (s *session) runCheck(ctx context.Context, in CheckInput) (*CheckResult, string, error) {
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
		Staged:       in.Staged,
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
	return checkResult(c), text, nil
}

// checkResult builds the tool result of the one-package check c: the
// package's report, passed only when c as a whole passed, and the module
// row's report when c has one.
func checkResult(c *report.Check) *CheckResult {
	res := &CheckResult{Report: c.Packages[0].Report}
	passed := !c.Failed()
	res.Passed = &passed
	if c.Module != nil {
		m := c.Module.Report
		res.Module = &m
	}
	return res
}

// checkText renders the one-package check c for the agent: a verdict line
// saying what to do next, then the check's text report, which lists the
// module row's findings under "<module>" as the CLI's text format does.
func checkText(c *report.Check) (string, error) {
	r := &c.Packages[0].Report
	var b strings.Builder
	if c.Failed() {
		b.WriteString("FAILED: " + r.PackagePath + " has " + strconv.Itoa(len(r.Violations)) + " violation(s)")
		if c.Module != nil && len(c.Module.Report.Violations) > 0 {
			b.WriteString(" and the module row has " + strconv.Itoa(len(c.Module.Report.Violations)))
		}
		b.WriteString(". Fix each one below, then call " + checkToolName + " again.\n")
	} else {
		b.WriteString("PASSED: " + r.PackagePath + " is no worse than its baseline.\n")
	}
	if err := report.WriteCheckText(&b, c); err != nil {
		return "", err
	}
	return b.String(), nil
}
