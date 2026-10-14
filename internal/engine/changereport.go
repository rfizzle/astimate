package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/narrate/markdown"
	"github.com/rfizzle/astimate/internal/report"
)

// ChangeOptions shape WriteChange.
type ChangeOptions struct {
	// Base is the ref the check ran against; empty for a check judged
	// against a baseline file, which the report calls "baseline".
	Base string
	// GitHub selects the GitHub annotations instead of Markdown.
	GitHub bool
	// SummaryFile, with GitHub, is the file the Markdown is appended to
	// (GITHUB_STEP_SUMMARY); empty writes none.
	SummaryFile string
}

// WriteChange renders the check c, already run on t's module, as the
// change block of the module report (SPEC.md 10.3): Markdown on w, or,
// with opts.GitHub, the workflow annotations of report.WriteGitHubRanked
// on w, ranked as the report ranks its failing packages and capped at
// report.MaxAnnotations of each kind, and the Markdown, with a closing
// line saying how many annotations the cap left out, appended to
// opts.SummaryFile. When the report cannot be built, the annotations are
// written in check order and the summary is left alone, so a failure to
// narrate never costs the annotations.
func WriteChange(ctx context.Context, t *Target, c *report.Check, opts ChangeOptions, w io.Writer) error {
	base := opts.Base
	if base == "" {
		base = "baseline"
	}
	build := func() (*narrate.Module, error) {
		m, _, err := Report(ctx, t, ReportOptions{Base: base, Check: c})
		return m, err
	}
	if !opts.GitHub {
		m, err := build()
		if err != nil {
			return err
		}
		return writeChangeMarkdown(w, m, "")
	}
	// Ranking needs the report, which extracts every package again; the
	// first pass in check order is final when the cap left nothing out and
	// no summary is wanted.
	var buf bytes.Buffer
	omitted, err := report.WriteGitHubRanked(&buf, c, nil)
	if err != nil {
		return err
	}
	var m *narrate.Module
	if omitted != (report.Omitted{}) || opts.SummaryFile != "" {
		var err error
		if m, err = build(); err != nil {
			t.logger().Error("building the report for the step summary failed", "err", err)
		} else if omitted != (report.Omitted{}) {
			buf.Reset()
			rank := make([]string, len(m.Change.Failing))
			for i, f := range m.Change.Failing {
				rank[i] = f.Path
			}
			if _, err := report.WriteGitHubRanked(&buf, c, rank); err != nil {
				return err
			}
		}
	}
	if text := report.OmittedText(omitted); text != "" {
		buf.WriteString("astimate: " + text + "\n")
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("writing github annotations: %w", err)
	}
	if m == nil || opts.SummaryFile == "" {
		return nil
	}
	return appendSummary(opts.SummaryFile, m, report.OmittedText(omitted))
}

// writeChangeMarkdown writes m's change block to w, then note as a line
// of its own when it is not empty.
func writeChangeMarkdown(w io.Writer, m *narrate.Module, note string) error {
	var buf bytes.Buffer
	if err := markdown.WriteChange(&buf, m, markdown.Options{}); err != nil {
		return err
	}
	if note != "" {
		buf.WriteString("\n" + note + ".\n")
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// appendSummary appends the change block of m, and note, to the step
// summary file at path.
func appendSummary(path string, m *narrate.Module, note string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening the step summary: %w", err)
	}
	if err := writeChangeMarkdown(f, m, note); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing the step summary %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing the step summary: %w", err)
	}
	return nil
}
