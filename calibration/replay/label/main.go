// Command label writes rule labels for a replayed history (SPEC.md 11.3):
// one label per commit of a replay data directory, block when the commit
// was reverted later in the range, or when a function it added or changed
// is changed again within the next 20 first-parent commits by a commit
// whose subject starts with fix or revert; allow otherwise. With --corpus
// and --name, each label also says whether the corpus entry's agent rule
// matched the commit. The labels file has the shape package labels reads,
// with provenance rule.
//
// Usage, from the repository root:
//
//	go run ./calibration/replay/label --repo <clone> --data <replay dir> --out <labels.yaml>
//	    [--corpus calibration/corpus-commits.yaml --name <entry>] [--window 20]
//
// See calibration/replay/README.md for the rule and its precision.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/rfizzle/astimate/calibration/replay/labels"
)

// defaultWindow is how many later first-parent commits the fix-up part of
// the rule looks at.
const defaultWindow = 20

// options are the parsed command-line flags: the string flags by name,
// and the window.
type options struct {
	str    map[string]*string
	window int
}

// get returns the string flag name.
func (o *options) get(name string) string { return *o.str[name] }

// flagUsage is the usage text of each string flag.
func flagUsage() map[string]string {
	return map[string]string{
		"repo":   "clone of the replayed repository",
		"data":   "replay data directory holding commits.jsonl and run.json",
		"out":    "labels file to write",
		"corpus": "corpus file whose entry's agent rule classifies commits",
		"name":   "the corpus entry, with --corpus",
	}
}

func main() {
	os.Exit(exitCode(os.Args[1:]))
}

// exitCode runs the command with args until done or interrupted: 0 on
// success, 1 after printing the error.
func exitCode(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, args, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "label:", err)
		return 1
	}
	return 0
}

// run parses args, labels the replayed commits and writes the labels file,
// printing the counts to stderr.
func run(ctx context.Context, args []string, stderr io.Writer) error {
	o, err := parseFlags(args, stderr)
	if err != nil {
		return err
	}
	var rule *agentRule
	if o.get("corpus") != "" {
		c, err := loadCorpus(o.get("corpus"))
		if err != nil {
			return err
		}
		entry, err := c.find(o.get("name"))
		if err != nil {
			return err
		}
		rule = &entry.Agent
	}
	f, err := labelData(ctx, gitRepo{dir: o.get("repo")}, o.get("data"), o.window, rule)
	if err != nil {
		return err
	}
	if err := labels.Write(o.get("out"), f); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stderr, summarize(f))
	return err
}

// parseFlags parses the command line.
func parseFlags(args []string, stderr io.Writer) (options, error) {
	o := options{str: map[string]*string{}, window: defaultWindow}
	fs := flag.NewFlagSet("label", flag.ContinueOnError)
	fs.SetOutput(stderr)
	for name, usage := range flagUsage() {
		o.str[name] = fs.String(name, "", usage)
	}
	fs.IntVar(&o.window, "window", o.window, "later first-parent commits the fix-up part looks at")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	switch {
	case fs.NArg() > 0:
		return o, fmt.Errorf("unexpected arguments: %v", fs.Args())
	case o.get("repo") == "" || o.get("data") == "" || o.get("out") == "":
		return o, errors.New("--repo, --data and --out are required")
	case (o.get("corpus") == "") != (o.get("name") == ""):
		return o, errors.New("--corpus and --name go together")
	case o.window < 1:
		return o, errors.New("--window must be at least 1")
	}
	return o, nil
}

// summarize counts f's labels: commits, agent commits, block labels and
// how many each part of the rule produced, alone and together.
func summarize(f *labels.File) string {
	var agent, block, revert, fixup, both int
	for i := range f.Commits {
		l := &f.Commits[i]
		if l.Agent != nil && *l.Agent {
			agent++
		}
		if l.Verdict == labels.Block {
			block++
		}
		r, x := fired(l, labels.PartRevert), fired(l, labels.PartFixup)
		revert, fixup = revert+b2i(r), fixup+b2i(x)
		both += b2i(r && x)
	}
	return fmt.Sprintf("commits=%d agent=%d block=%d revert=%d fixup=%d both=%d",
		len(f.Commits), agent, block, revert, fixup, both)
}

// fired reports whether part of the rule fired on l.
func fired(l *labels.Label, part string) bool {
	for _, e := range l.Fired {
		if e.Part == part {
			return true
		}
	}
	return false
}

// b2i is 1 for true.
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
