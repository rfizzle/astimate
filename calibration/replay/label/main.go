// Command label writes rule labels for a replayed history (SPEC.md 11.3):
// one label per commit of a replay data directory, by one of two rules.
//
// The fix-up rule (--rule fixup, the default) marks a commit block when it
// was reverted later in the range, or when a function it added or changed
// is changed again within the next 20 first-parent commits by a commit
// whose subject starts with fix or revert. It reads the history from a
// clone (--repo).
//
// The split-or-extraction rule (--rule split-extract) marks a commit block
// when a later commit of the range split a package the commit took over a
// capacity max (or grew by 100 or more SLOC while over one), or extracted
// duplicate blocks the commit added; --window caps how far it looks. It
// reads only the replay's rows, with the capacity rules of the
// configuration the replay gated with (--config overrides).
//
// Either rule labels allow otherwise. With --corpus and --name, each label
// also says whether the corpus entry's agent rule matched the commit. The
// labels file has the shape package labels reads, with provenance rule.
//
// Usage, from the repository root:
//
//	go run ./calibration/replay/label --repo <clone> --data <replay dir> --out <labels.yaml>
//	    [--corpus calibration/corpus-commits.yaml --name <entry>] [--window 20]
//	go run ./calibration/replay/label --rule split-extract --data <replay dir> --out <labels.yaml>
//	    [--config <file>] [--corpus calibration/corpus-commits.yaml --name <entry>] [--window N]
//
// See calibration/replay/README.md for the rules and their precision.
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

// defaultWindow is how many later first-parent commits the fix-up rule
// looks at.
const defaultWindow = 20

// The labeling rules --rule names.
const (
	ruleFixup        = "fixup"
	ruleSplitExtract = "split-extract"
)

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
		"rule":   "labeling rule: fixup (revert or later fix) or split-extract (later split or extraction)",
		"repo":   "clone of the replayed repository, for the fixup rule",
		"data":   "replay data directory holding commits.jsonl and run.json (and packages.jsonl for split-extract)",
		"out":    "labels file to write",
		"config": "for split-extract, the configuration whose capacity rules apply; empty means the one the replay gated with",
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
	var f *labels.File
	parts := [2]string{labels.PartRevert, labels.PartFixup}
	if o.get("rule") == ruleSplitExtract {
		parts = [2]string{labels.PartSplit, labels.PartExtract}
		f, err = labelSplit(o.get("data"), o.get("config"), o.window, rule)
	} else {
		f, err = labelData(ctx, gitRepo{dir: o.get("repo")}, o.get("data"), o.window, rule)
	}
	if err != nil {
		return err
	}
	if err := labels.Write(o.get("out"), f); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stderr, summarize(f, parts))
	return err
}

// parseFlags parses the command line. The window defaults to the rule's:
// 20 for fixup, and 0, the rest of the range, for split-extract.
func parseFlags(args []string, stderr io.Writer) (options, error) {
	o := options{str: map[string]*string{}}
	fs := flag.NewFlagSet("label", flag.ContinueOnError)
	fs.SetOutput(stderr)
	for name, usage := range flagUsage() {
		o.str[name] = fs.String(name, "", usage)
	}
	*o.str["rule"] = ruleFixup
	fs.IntVar(&o.window, "window", 0, "later first-parent commits the rule looks at (default 20 for fixup, the rest of the range for split-extract)")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	windowSet := false
	fs.Visit(func(f *flag.Flag) { windowSet = windowSet || f.Name == "window" })
	split := o.get("rule") == ruleSplitExtract
	if !windowSet && !split {
		o.window = defaultWindow
	}
	switch {
	case fs.NArg() > 0:
		return o, fmt.Errorf("unexpected arguments: %v", fs.Args())
	case o.get("rule") != ruleFixup && !split:
		return o, fmt.Errorf("--rule %q is not %s or %s", o.get("rule"), ruleFixup, ruleSplitExtract)
	case o.get("data") == "" || o.get("out") == "":
		return o, errors.New("--data and --out are required")
	case !split && o.get("repo") == "":
		return o, errors.New("--repo is required for the fixup rule")
	case !split && o.get("config") != "":
		return o, errors.New("--config applies to the split-extract rule only")
	case (o.get("corpus") == "") != (o.get("name") == ""):
		return o, errors.New("--corpus and --name go together")
	case windowSet && o.window < 1:
		return o, errors.New("--window must be at least 1")
	}
	return o, nil
}

// summarize counts f's labels: commits, agent commits, block labels and
// how many each of the rule's two parts produced, alone and together.
func summarize(f *labels.File, parts [2]string) string {
	var agent, block, first, second, both int
	for i := range f.Commits {
		l := &f.Commits[i]
		if l.Agent != nil && *l.Agent {
			agent++
		}
		if l.Verdict == labels.Block {
			block++
		}
		a, b := fired(l, parts[0]), fired(l, parts[1])
		first, second = first+b2i(a), second+b2i(b)
		both += b2i(a && b)
	}
	return fmt.Sprintf("commits=%d agent=%d block=%d %s=%d %s=%d both=%d",
		len(f.Commits), agent, block, parts[0], first, parts[1], second, both)
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
