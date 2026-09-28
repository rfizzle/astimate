// Command rebuild defines the rebuild experiment of SPEC.md 11.2: which
// corpus packages, or directory trees of packages, are deleted and
// rebuilt, how each is stubbed, and the oracle a rebuild must pass. See
// calibration/rebuild/README.md.
//
// Usage, from the repository root:
//
//	go run ./calibration/rebuild select [flags]   # choose and verify the packages (network)
//	go run ./calibration/rebuild select --unit tree [flags]   # choose and verify directory trees (network)
//	go run ./calibration/rebuild stub --root <module root> --dir <package dir> [--tree] [--sha256 <hash>]
//	go run ./calibration/rebuild run --live [flags]  # run every experiment with Claude Code (live requests)
//	go run ./calibration/rebuild run --agent <template> [flags]  # run with another agent command
//
// select reads the corpus data, clones each candidate's module at its pin
// into a scratch directory, verifies the candidate and writes rebuild.yaml
// and selection.md (rebuild-trees.yaml and selection-trees.md for trees).
// stub stubs one package, or a tree, of a module in place and prints the
// tree hash. run clones, stubs and hands each experiment to an agent, runs
// the oracle and appends one row per run to runs.jsonl; without --live it
// never starts the default agent, Claude Code.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/pin"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/stub"
)

// Exit codes: 0 on success, 1 when the command ran and failed, 2 for a
// usage error.
const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run dispatches the subcommand in args and returns the exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: rebuild select|stub|run [flags]")
		return exitUsage
	}
	var err error
	switch args[0] {
	case "select":
		err = runSelect(ctx, args[1:], stdout, stderr)
	case "stub":
		err = runStub(ctx, args[1:], stdout, stderr)
	case "run":
		err = runRuns(ctx, args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "rebuild: unknown command %q; want select, stub or run\n", args[0])
		return exitUsage
	}
	switch {
	case errors.Is(err, flag.ErrHelp):
		return exitUsage
	case errors.Is(err, errUsage):
		_, _ = fmt.Fprintln(stderr, "rebuild:", err)
		return exitUsage
	case err != nil:
		_, _ = fmt.Fprintln(stderr, "rebuild:", err)
		return exitFail
	}
	return exitOK
}

// runStub stubs one package, or with --tree the package and every
// non-main package below it, in place.
func runStub(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("stub", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "module root")
	dir := fs.String("dir", "", "package directory relative to the module root")
	tree := fs.Bool("tree", false, "stub the package and every non-main package below it, as a tree experiment does")
	want := fs.String("sha256", "", "expected tree hash; the stub is not written when it differs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		fs.Usage()
		return flag.ErrHelp
	}
	env := pin.DefaultEnv()
	pkgDir := filepath.Join(*root, filepath.FromSlash(*dir))
	var files []stub.File
	var err error
	if *tree {
		var dirs []string
		if dirs, err = pin.TreeMembers(ctx, *root, *dir, env); err == nil {
			files, err = stub.Tree(ctx, *root, *dir, dirs, env)
		}
	} else {
		files, err = stub.Package(ctx, pkgDir, env)
	}
	if err != nil {
		return err
	}
	hash := stub.TreeHash(files)
	if *want != "" && hash != *want {
		return fmt.Errorf("tree hash %s, want %s", hash, *want)
	}
	if err := stub.Write(pkgDir, files); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, hash)
	return err
}
