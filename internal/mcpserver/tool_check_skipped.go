package mcpserver

import (
	"log/slog"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/report"
)

// SkippedResult is the structured content of a check_package call on a
// package that does not load, such as one importing a module that is not
// available: the gate cannot judge it, which is a result, not an error.
type SkippedResult struct {
	// Skipped names the package, the kind of failure and its first error.
	Skipped report.Skipped `json:"skipped"`
}

// withSkippedBranch adds SkippedResult to the oneOf of t's output schema,
// so a skipped package's result matches exactly one branch, and returns t.
// A tool without an output schema is returned unchanged; inference failing
// is logged as withOutputSchema logs it.
func withSkippedBranch(t *mcp.Tool, logger *slog.Logger) *mcp.Tool {
	if t.OutputSchema == nil {
		return t
	}
	skipped, err := jsonschema.For[SkippedResult](nil)
	if err != nil {
		logger.Error("tool registered without the skipped result schema", "tool", t.Name, "err", err)
		return t
	}
	schema, ok := t.OutputSchema.(*jsonschema.Schema)
	if !ok {
		return t
	}
	schema.OneOf = append(schema.OneOf, skipped)
	return t
}

// skippedResult returns the result of checking dir, a package c selected,
// when the check could not judge it because it does not load: every error
// of failed is engine.IsSkipped and c names dir among its skipped
// packages. It returns false otherwise.
func skippedResult(c *report.Check, failed []error, dir string) (*SkippedResult, string, bool) {
	if c == nil || len(failed) == 0 || slices.ContainsFunc(failed, func(err error) bool { return !engine.IsSkipped(err) }) {
		return nil, "", false
	}
	i := slices.IndexFunc(c.Skipped, func(s report.Skipped) bool { return s.Package == dir })
	if i < 0 {
		return nil, "", false
	}
	s := c.Skipped[i]
	text := "SKIPPED: " + s.Package + " does not load (" + s.Reason + "), so the gate cannot judge it. " +
		"Make it load, then call " + checkToolName + " again. First error: " + s.Error + "\n"
	return &SkippedResult{Skipped: s}, text, true
}
