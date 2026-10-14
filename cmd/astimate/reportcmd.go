package main

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/cli"
	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/narrate/render"
)

// runReport prints the module report of a module: `report [<module-root>]
// [--base ref] [--format text|json|md|html] [--all] [--out file] [--open]
// [--color auto|always|never] [--config path] [--tokenizer est|o200k]`
// (SPEC.md 10.3). The root defaults to the current directory. A package
// that fails is logged and left out, and the command exits 2 after
// printing the rest. A failing change is reported, not gated.
func runReport(args []string, stdout, stderr io.Writer) int {
	formats := strings.Join(render.Formats(), "|")
	fs := cli.NewFlagSet("report", "usage: astimate report [<module-root>] [--base ref] [--format "+formats+
		"] [--all] [--out file] [--open] [--color auto|always|never] [--config path] [--tokenizer est|o200k]", stderr)
	base := fs.String("base", "", "add the change against the merge-base of HEAD and this git ref")
	format := fs.String("format", render.FormatText, "output format: "+strings.Join(render.Formats(), ", "))
	opts := render.Flags(fs)
	color := fs.String("color", cli.ColorAuto, "colour the text format: auto (a terminal without NO_COLOR), always or never")
	var tf targetFlags
	tf.register(fs, false)
	dir, ok := cli.ParseModuleArgs("report", fs, args, stderr)
	if !ok {
		return exitUsage
	}
	if _, ok := tf.validate("report", stderr); !ok {
		return exitUsage
	}
	if !slices.Contains(render.Formats(), *format) {
		_, _ = fmt.Fprintf(stderr, "astimate: report: unknown format %q: want one of %s\n",
			*format, strings.Join(render.Formats(), ", "))
		return exitUsage
	}
	var err error
	if opts.Width, opts.Color, err = cli.Terminal(stdout, *color); err != nil {
		_, _ = fmt.Fprintf(stderr, "astimate: report: %v\n", err)
		return exitUsage
	}
	logger := cli.NewLogger(stderr)
	t, err := tf.load(dir, logger)
	if err != nil {
		logger.Error("report failed", "dir", dir, "err", err)
		return exitAnalysis
	}
	ctx := context.Background()
	m, failed, err := engine.Report(ctx, t, engine.ReportOptions{Base: *base})
	if err != nil {
		logger.Error("report failed", "dir", dir, "err", err)
		return exitAnalysis
	}
	return emit("report", t, failed, stdout, logger, func(w io.Writer) error {
		return render.Write(ctx, w, m, *format, opts)
	})
}
