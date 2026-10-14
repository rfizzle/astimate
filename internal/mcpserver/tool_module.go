package mcpserver

import (
	"context"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/narrate"
)

// moduleToolName is the name the module report tool is registered under
// (SPEC.md 10.1).
const moduleToolName = "report_module"

// moduleToolDescription tells the agent what report_module is for.
const moduleToolDescription = "Report on a whole Go module in plain language, as `astimate report` prints it: " +
	"a verdict (headline and lead) on the repository, the three packages to start with (start_here, each " +
	"with its story, what is wrong, and its do sentence, what to do about it) and the full module report " +
	"(every package's band, rebuild cost, copies, complexity, tests and churn). Call it to answer " +
	"\"what should I split first\" in one call. With base, a git ref, it also checks the change since " +
	"that ref. Slower than assess_package: it analyzes every package."

// ModuleInput is the input of the report_module tool.
type ModuleInput struct {
	// Path is the module's root directory or any directory inside it.
	Path string `json:"path,omitempty" jsonschema:"Directory of the module to report on (or any directory inside it), relative to the server's working directory or absolute. Defaults to the working directory."`
	// Base is the git ref the change section is checked against.
	Base string `json:"base,omitempty" jsonschema:"Git ref to check the change against, such as master. Omit for the repository alone, without a change section."`
}

// startHereItem is one of the packages to start with.
type startHereItem struct {
	// Path is the package directory relative to the module root.
	Path string `json:"path"`
	// Band is split, watch or fine.
	Band string `json:"band"`
	// Story says what is wrong with the package.
	Story string `json:"story"`
	// Do says what to do about it.
	Do string `json:"do"`
}

// moduleResult is the structured content of a successful report_module
// call.
type moduleResult struct {
	// Headline and Lead are the report's verdict on the repository.
	Headline string `json:"headline"`
	Lead     string `json:"lead"`
	// StartHere are the first three hotspots, as the report's "Start here"
	// lists them.
	StartHere []startHereItem `json:"start_here"`
	// Report is the module report (SPEC.md 10.3).
	Report *narrate.Module `json:"report"`
	// Failed are the packages left out because they did not extract.
	Failed []rankFailure `json:"failed"`
}

// addModuleTool registers report_module on srv, backed by s.
func addModuleTool(srv *mcp.Server, s *session) {
	tool := &mcp.Tool{Name: moduleToolName, Title: "Report on a module in plain language", Description: moduleToolDescription}
	mcp.AddTool(srv, withOutputSchema[moduleResult](tool, s.opts.logger()), s.reportModule)
}

// reportModule handles a report_module call. Packages that fail to
// extract are listed under failed, not an error; an input, resolution or
// check failure is a result with isError set and structured content
// {"error": ...}.
func (s *session) reportModule(ctx context.Context, _ *mcp.CallToolRequest, in ModuleInput) (*mcp.CallToolResult, any, error) {
	res, err := s.runModule(ctx, in)
	if err != nil {
		return s.failure(moduleToolName, moduleToolName+": "+err.Error(), err, "path", in.Path)
	}
	return textResult(res.text()), res, nil
}

// runModule loads the module through the session cache and builds its
// report.
func (s *session) runModule(ctx context.Context, in ModuleInput) (moduleResult, error) {
	dir, err := s.opts.resolvePath(in.Path)
	if err != nil {
		return moduleResult{}, err
	}
	t, rc, err := s.loadTarget(dir)
	if err != nil {
		return moduleResult{}, err
	}
	defer rc.mu.Unlock()
	m, failed, err := engine.Report(ctx, t, engine.ReportOptions{Base: in.Base})
	if err != nil {
		return moduleResult{}, err
	}
	return moduleResult{Headline: m.Headline, Lead: m.Lead, Report: m, StartHere: startHere(m), Failed: failures(failed)}, nil
}

// startHere returns the first three hotspots of m with their stories and
// do sentences.
func startHere(m *narrate.Module) []startHereItem {
	byPath := make(map[string]*narrate.Package, len(m.Packages))
	for i := range m.Packages {
		byPath[m.Packages[i].Path] = &m.Packages[i]
	}
	out := make([]startHereItem, 0, 3)
	for _, path := range m.Hotspots[:min(3, len(m.Hotspots))] {
		if p := byPath[path]; p != nil {
			out = append(out, startHereItem{Path: path, Band: p.Band, Story: p.Story, Do: p.Todo})
		}
	}
	return out
}

// text renders r for the agent: the verdict, then each package to start
// with, its story and what to do, then the packages left out.
func (r moduleResult) text() string {
	var b strings.Builder
	b.WriteString(r.Headline + "\n" + r.Lead + "\n")
	if len(r.StartHere) > 0 {
		b.WriteString("\nStart here:\n")
	}
	for i, h := range r.StartHere {
		b.WriteString(strconv.Itoa(i+1) + ". " + h.Path + " (" + h.Band + ")\n   " + h.Story + "\n   Do this: " + h.Do + "\n")
	}
	if len(r.Failed) > 0 {
		b.WriteString("\nskipped " + strconv.Itoa(len(r.Failed)) + " packages:\n")
		for _, f := range r.Failed {
			b.WriteString("  " + f.Path + ": " + f.Error + "\n")
		}
	}
	return b.String()
}
