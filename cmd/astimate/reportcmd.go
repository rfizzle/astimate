package main

import (
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/narrate/html"
	"github.com/rfizzle/astimate/internal/narrate/markdown"
)

// runReport prints the module report of a module: `report [<module-root>]
// [--base ref] [--format json] [--config path] [--tokenizer est|o200k]`
// (SPEC.md 10.3). The root defaults to the current directory. Without
// --format it notes on stderr that text output is coming and prints the
// JSON. A package that fails is logged and left out, and the command exits
// 2 after printing the rest. A failing change is reported, not gated.
func runReport(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("report", "usage: astimate report [<module-root>] [--base ref] [--format json|md|html] "+
		"[--out file] [--open] [--config path] [--tokenizer est|o200k]", stderr)
	base := fs.String("base", "", "add the change against the merge-base of HEAD and this git ref")
	format := fs.String("format", "", "output format: json, md, html")
	page := html.Flags(fs)
	var tf targetFlags
	tf.register(fs, false)
	dir, ok := parseModuleArgs("report", fs, args, stderr)
	if !ok {
		return exitUsage
	}
	if _, ok := tf.validate("report", stderr); !ok {
		return exitUsage
	}
	if !slices.Contains([]string{"json", "md", "html", ""}, *format) {
		_, _ = fmt.Fprintf(stderr, "astimate: report: unknown format %q: want json, md or html\n", *format)
		return exitUsage
	}
	if *format == "" {
		_, _ = fmt.Fprintln(stderr, "astimate: report: text output is coming; printing JSON (--format json)")
	}
	logger := newLogger(stderr)
	t, err := tf.load(dir, logger)
	var m *narrate.Module
	var failed []error
	if err == nil {
		m, failed, err = engine.Report(context.Background(), t, engine.ReportOptions{Base: *base})
	}
	if err != nil {
		logger.Error("report failed", "dir", dir, "err", err)
		return exitAnalysis
	}
	return emit("report", t, failed, stdout, logger, func(w io.Writer) error {
		if *format == "html" {
			return html.Save(context.Background(), m, *page, w)
		}
		if *format == "md" {
			return markdown.Write(w, m, markdown.Options{})
		}
		return narrate.WriteJSON(w, m)
	})
}
