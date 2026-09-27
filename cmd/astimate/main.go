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

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

// run executes the CLI with args (excluding the program name) and returns the
// process exit code. Usage and diagnostics go to stderr.
func run(args []string, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return exitUsage
	}
	_, _ = fmt.Fprintf(stderr, "astimate: unknown command %q\n\n%s", args[0], usage)
	return exitUsage
}
