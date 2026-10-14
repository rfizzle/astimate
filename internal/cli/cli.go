// Package cli holds the plumbing every astimate command shares: its flag
// set, the module root among its arguments, its logger and its buffered
// output, and the width and colour of its text reports. It imports the
// standard library and golang.org/x/term only.
package cli

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log/slog"
)

// NewFlagSet returns the flag set of command name, "astimate <name>",
// which prints its errors to stderr and, on -h or a usage error, usageLine
// followed by the flag defaults.
func NewFlagSet(name, usageLine string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("astimate "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, usageLine)
		fs.PrintDefaults()
	}
	return fs
}

// ParseInterspersed parses args with fs, accepting flags before and after
// positional arguments, and returns the positional arguments in order.
func ParseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// ModuleRoot returns the module root among a command's positional
// arguments, "." when there is none. With more than one it prints the
// usage error of command name and fs's usage to stderr and returns false.
func ModuleRoot(name string, positional []string, fs *flag.FlagSet, stderr io.Writer) (string, bool) {
	switch len(positional) {
	case 0:
		return ".", true
	case 1:
		return positional[0], true
	}
	_, _ = fmt.Fprintf(stderr, "astimate: %s: want at most one module root, got %d arguments\n", name, len(positional))
	fs.Usage()
	return "", false
}

// ParseModuleArgs parses args, flags and positional arguments interleaved,
// with fs and returns the module root among the positional arguments
// (ModuleRoot). On a usage error, which the flag package or ModuleRoot has
// printed, it returns false.
func ParseModuleArgs(name string, fs *flag.FlagSet, args []string, stderr io.Writer) (string, bool) {
	positional, err := ParseInterspersed(fs, args)
	if err != nil {
		return "", false
	}
	return ModuleRoot(name, positional, fs, stderr)
}

// NewLogger returns the logger every command logs to stderr with, at info
// level.
func NewLogger(stderr io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// WriteBuffered renders into a buffer with render and writes it to stdout
// in one call, so a render error leaves stdout empty and a write error
// cannot leave a partial report.
func WriteBuffered(stdout io.Writer, render func(io.Writer) error) error {
	var buf bytes.Buffer
	if err := render(&buf); err != nil {
		return err
	}
	_, err := stdout.Write(buf.Bytes())
	return err
}
