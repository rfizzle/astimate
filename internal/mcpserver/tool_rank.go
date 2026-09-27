package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/report"
)

// rankToolName is the name the rank tool is registered under (SPEC.md
// 10.1).
const rankToolName = "rank_packages"

// rankToolDescription tells the agent what rank_packages is for.
const rankToolDescription = "Rank every package of a Go module by estimated rebuild effort, so you can " +
	"see where technical debt is concentrated before choosing what to touch. Each row gives the " +
	"package path, agent_passes, human_days, tier, fan_in, tokens_est and duplication_pct, sorted " +
	"descending by the sort key. Packages that fail to analyze are listed as skipped; the rest are " +
	"still ranked."

// RankInput is the input of the rank_packages tool.
type RankInput struct {
	// ModuleRoot is the module's root directory or any directory inside it.
	ModuleRoot string `json:"module_root,omitempty" jsonschema:"Directory of the module to rank (or any directory inside it), relative to the server's working directory or absolute. Defaults to the working directory."`
	// Top keeps only the first Top rows; 0 keeps every row.
	Top int `json:"top,omitempty" jsonschema:"Return only the first top rows after sorting. 0 or omitted returns every package."`
	// Sort is the sort key, one of report.SortKeys.
	Sort string `json:"sort,omitempty" jsonschema:"Sort key, descending: passes (default), days, fan_in, tokens or duplication."`
}

// rankFailure is one package rank_packages skipped.
type rankFailure struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// rankResult is the structured content of a successful rank_packages call.
type rankResult struct {
	Rows   []report.Row  `json:"rows"`
	Failed []rankFailure `json:"failed"`
}

// addRankTool registers rank_packages on srv, backed by s.
func addRankTool(srv *mcp.Server, s *session) {
	mcp.AddTool(srv, withOutputSchema[rankResult](&mcp.Tool{
		Name:        rankToolName,
		Title:       "Rank a module's packages by rebuild effort",
		Description: rankToolDescription,
	}, s.opts.logger()), s.rankPackages)
}

// rankPackages handles a rank_packages call. Packages that fail to extract
// are reported in the text and under failed, not as an error; an input,
// resolution or listing failure is a result with isError set and
// structured content {"error": ...}.
func (s *session) rankPackages(ctx context.Context, _ *mcp.CallToolRequest, in RankInput) (*mcp.CallToolResult, any, error) {
	text, res, err := s.runRank(ctx, in)
	if err != nil {
		msg := rankToolName + ": " + err.Error()
		s.opts.logger().Warn("tool call failed", "tool", rankToolName, "module_root", in.ModuleRoot, "err", err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: msg}},
			IsError: true,
		}, checkError{Error: msg}, nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, res, nil
}

// runRank validates in, loads the module through the session cache, ranks
// its packages and renders the result.
func (s *session) runRank(ctx context.Context, in RankInput) (string, rankResult, error) {
	key := in.Sort
	if key == "" {
		key = report.SortPasses
	}
	if !slices.Contains(report.SortKeys(), key) {
		return "", rankResult{}, fmt.Errorf("%w %q: want one of %s",
			report.ErrUnknownSortKey, key, strings.Join(report.SortKeys(), ", "))
	}
	if in.Top < 0 {
		return "", rankResult{}, fmt.Errorf("top is %d; want 0 (every package) or more", in.Top)
	}
	dir, err := s.opts.resolvePath(in.ModuleRoot)
	if err != nil {
		return "", rankResult{}, fmt.Errorf("module_root: %w", err)
	}
	t, rc, err := s.loadTarget(dir)
	if err != nil {
		return "", rankResult{}, err
	}
	defer rc.mu.Unlock()
	rows, failed, err := engine.Rank(ctx, t, engine.RankOptions{Sort: key, Top: in.Top})
	if err != nil {
		return "", rankResult{}, err
	}
	return rankOutput(rows, failed)
}

// rankOutput builds the text and structured content of a ranking: the
// table, then, when any package failed, a "skipped N packages:" list with
// each error.
func rankOutput(rows []report.Row, failed []error) (string, rankResult, error) {
	res := rankResult{Rows: rows, Failed: make([]rankFailure, 0, len(failed))}
	if res.Rows == nil {
		res.Rows = []report.Row{}
	}
	var b strings.Builder
	if err := report.WriteRowsTable(&b, rows); err != nil {
		return "", rankResult{}, err
	}
	if len(failed) > 0 {
		b.WriteString("\nskipped " + strconv.Itoa(len(failed)) + " packages:\n")
	}
	for _, err := range failed {
		f := rankFailure{Error: err.Error()}
		var pe *engine.PackageError
		if errors.As(err, &pe) {
			f.Path, f.Error = pe.Path, pe.Err.Error()
		}
		res.Failed = append(res.Failed, f)
		b.WriteString("  " + err.Error() + "\n")
	}
	return b.String(), res, nil
}
