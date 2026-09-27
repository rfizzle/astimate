package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/rfizzle/astimate/internal/config"
)

const configUsage = `usage: astimate config <subcommand> [arguments]

Subcommands:
  init  write the default configuration file
`

const defaultConfigPath = "astimate.yaml"

// configCommands is the dispatch table for `astimate config <subcommand>`.
func configCommands() map[string]command {
	return map[string]command{
		"init": runConfigInit,
	}
}

// runConfig dispatches `astimate config` to its subcommands.
func runConfig(args []string, stdout, stderr io.Writer) int {
	return dispatch(configCommands(), configUsage, args, stdout, stderr)
}

// runConfigInit writes the embedded default configuration to --out, refusing
// to replace an existing file unless --force is given.
func runConfigInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("astimate config init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", defaultConfigPath, "path of the configuration file to write")
	force := fs.Bool("force", false, "overwrite the file if it already exists")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "astimate: config init: unexpected argument %q\n", fs.Arg(0))
		fs.Usage()
		return exitUsage
	}

	if err := writeConfig(*out, config.Default(), *force); err != nil {
		if errors.Is(err, os.ErrExist) {
			_, _ = fmt.Fprintf(stderr, "astimate: config init: %s already exists; use --force to overwrite\n", *out)
		} else {
			_, _ = fmt.Fprintf(stderr, "astimate: config init: %v\n", err)
		}
		return exitUsage
	}
	_, _ = fmt.Fprintf(stdout, "wrote %s\n", *out)
	return exitOK
}

// writeConfig writes data to path with mode 0644. Without force it fails with
// an error wrapping os.ErrExist when path already exists.
func writeConfig(path string, data []byte, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", path, err)
	}
	return nil
}
