// Command astimate is a quality gate for Go packages. It extracts package
// metrics, compares them against a baseline and thresholds, and fails when a
// package got worse.
package main

import (
	"fmt"
	"io"
	"os"
)

const usage = `usage: astimate <command> [arguments]

Commands:
  check           gate changed packages against a baseline and thresholds
  baseline write  write a baseline file for the module
  assess          score one package
  rank            rank packages in a module
  serve           start the MCP server on stdio
  config init     write a default configuration file
  version         print the version
`

// command runs one subcommand with the arguments that follow its name and
// returns the process exit code.
type command func(args []string, stdout, stderr io.Writer) int

// commands is the top-level dispatch table. Adding a command is one entry.
func commands() map[string]command {
	return map[string]command{
		"assess":  runAssess,
		"config":  runConfig,
		"version": runVersion,
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the CLI with args (excluding the program name) and returns the
// process exit code. Command output goes to stdout; usage and diagnostics go
// to stderr.
func run(args []string, stdout, stderr io.Writer) int {
	return dispatch(commands(), usage, args, stdout, stderr)
}

// dispatch looks up args[0] in table and runs it with the remaining args. A
// missing or unknown name prints usageText to stderr and returns exitUsage.
func dispatch(table map[string]command, usageText string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usageText)
		return exitUsage
	}
	cmd, ok := table[args[0]]
	if !ok {
		_, _ = fmt.Fprintf(stderr, "astimate: unknown command %q\n\n%s", args[0], usageText)
		return exitUsage
	}
	return cmd(args[1:], stdout, stderr)
}
