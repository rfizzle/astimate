package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// errUsage marks an error the run command reports with exit code 2: bad
// flags, or a setup problem found before any run started.
var errUsage = errors.New("usage")

// runOptions are the run command's flags.
type runOptions struct {
	definition  string
	out         string
	repeats     int
	only        string
	agent       string
	agentSet    bool
	agentName   string
	model       string
	live        bool
	timeout     time.Duration
	parallel    int
	keep        bool
	transcripts bool
	plan        bool
}

// parseRunFlags parses the run command's flags.
func parseRunFlags(args []string, stderr io.Writer) (runOptions, error) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o runOptions
	fs.StringVar(&o.definition, "definition", "calibration/rebuild/rebuild.yaml", "experiment definition")
	fs.StringVar(&o.out, "out", "", "output directory for runs.jsonl and run.json (default calibration/data/rebuild-<date>-<agent name>)")
	fs.IntVar(&o.repeats, "repeats", 3, "runs per experiment")
	fs.StringVar(&o.only, "only", "", "run only the experiment of this package import path, or of this module")
	fs.StringVar(&o.agent, "agent", "", "agent command template run by sh -c in the module root, with placeholders {"+
		strings.Join(placeholders(), "}, {")+"} (default: Claude Code in print mode, which needs --live)")
	fs.StringVar(&o.agentName, "agent-name", "", "agent name for the rows and the default output directory (default "+
		defaultAgentName+", or "+customAgentName+" with --agent)")
	fs.StringVar(&o.model, "model", defaultModel, "model the agent is asked to use ({model})")
	fs.BoolVar(&o.live, "live", false, "allow an agent command that runs Claude Code (the default one does): this spends live requests")
	fs.DurationVar(&o.timeout, "timeout", 60*time.Minute, "wall-clock limit of one agent invocation")
	fs.IntVar(&o.parallel, "parallel", 1, "runs at a time")
	fs.BoolVar(&o.keep, "keep", false, "keep each run's clone instead of removing it")
	fs.BoolVar(&o.transcripts, "transcripts", true, "keep each agent's standard output under <out>/transcripts")
	fs.BoolVar(&o.plan, "plan", false, "print the pending runs and the most requests they may make, and run nothing")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "agent" {
			o.agentSet = true
		}
	})
	switch {
	case fs.NArg() > 0:
		return o, fmt.Errorf("%w: unexpected arguments: %s", errUsage, strings.Join(fs.Args(), " "))
	case o.repeats < 1:
		return o, fmt.Errorf("%w: --repeats must be at least 1", errUsage)
	case o.parallel < 1:
		return o, fmt.Errorf("%w: --parallel must be at least 1", errUsage)
	case o.timeout <= 0:
		return o, fmt.Errorf("%w: --timeout must be positive", errUsage)
	case o.agentSet && strings.TrimSpace(o.agent) == "":
		return o, fmt.Errorf("%w: --agent is empty", errUsage)
	}
	if !o.agentSet {
		o.agent = defaultAgentTemplate
	}
	if err := checkTemplate(o.agent); err != nil {
		return o, fmt.Errorf("%w: %w", errUsage, err)
	}
	if o.agentName == "" {
		o.agentName = defaultAgentName
		if o.agentSet {
			o.agentName = customAgentName
		}
	}
	if o.out == "" {
		o.out = filepath.Join("calibration", "data", "rebuild-"+time.Now().Format(time.DateOnly)+"-"+o.agentName)
	}
	return o, nil
}

// liveRefusal is the message the run command exits with when it would
// start Claude Code without --live.
const liveRefusal = "the agent command runs Claude Code, which spends live requests; " +
	"add --live to run it (see calibration/rebuild/README.md for the commands and what they cost), " +
	"--plan to see what would run, or --agent <template> to run another command"

// callsClaude reports whether the agent template runs the claude command:
// any word of it whose base name is claude.
func callsClaude(tmpl string) bool {
	for _, w := range strings.FieldsFunc(tmpl, func(r rune) bool {
		return strings.ContainsRune(" \t\n;&|()`'\"$<>{}", r)
	}) {
		if filepath.Base(w) == "claude" {
			return true
		}
	}
	return false
}

// runRuns runs the rebuild experiments.
func runRuns(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	o, err := parseRunFlags(args, stderr)
	if err != nil {
		return err
	}
	def, err := LoadDefinition(o.definition)
	if err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	exps, err := selectExperiments(def.Experiments, o.only)
	if err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if o.plan {
		return printPlan(stdout, filepath.Join(o.out, runsFile), exps, o)
	}
	// The one gate on spending: nothing below starts Claude Code, as the
	// default agent or from a template that calls it, unless the user
	// asked for it.
	if !o.live && callsClaude(o.agent) {
		return fmt.Errorf("%w: %s", errUsage, liveRefusal)
	}
	if !o.agentSet {
		if _, err := exec.LookPath("claude"); err != nil {
			return fmt.Errorf("%w: claude is not on PATH: %w", errUsage, err)
		}
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	gover, err := goVersion(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	rn := &runner{
		def: def, out: o.out, agentName: o.agentName, template: o.agent, model: o.model,
		timeout: o.timeout, keep: o.keep, transcripts: o.transcripts, goVersion: gover,
		clone: CloneAt, logger: logger,
	}
	info := RunInfo{
		StartedAt: time.Now().UTC(), GoVersion: gover, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, CPUs: runtime.NumCPU(),
		AstimateCommit: astimateCommit(ctx), Definition: filepath.ToSlash(o.definition),
		DefinitionConfigVersion: def.ConfigVersion, DefinitionGoVersion: def.GoVersion, Env: def.Env,
		Agent: o.agentName, Template: o.agent, Model: o.model, Repeats: o.repeats, Parallel: o.parallel,
		TimeoutSeconds: o.timeout.Seconds(), Only: o.only, Experiments: len(exps),
	}
	res, err := rn.resume(ctx, exps, o.repeats, o.parallel)
	if err != nil {
		return err
	}
	info.Skipped, info.Written, info.Failures = res.skipped, res.written, res.failures
	info.FinishedAt = time.Now().UTC()
	info.Totals = totals(res.rows)
	if err := writeJSON(filepath.Join(o.out, runInfoFile), info); err != nil {
		return err
	}
	logger.Info("done", "written", info.Written, "failed", len(info.Failures), "rows", info.Totals.Rows,
		"passed", info.Totals.Passed)
	switch {
	case ctx.Err() != nil:
		return fmt.Errorf("interrupted after %d runs; rerun the same command to resume", info.Written)
	case len(info.Failures) > 0:
		return fmt.Errorf("%d runs wrote no row; rerun the same command to retry them", len(info.Failures))
	}
	return nil
}

// printPlan prints the runs an invocation would start, skipping those
// already in the rows file, and the most agent turns they may take.
func printPlan(w io.Writer, rowsPath string, exps []Experiment, o runOptions) error {
	var done map[runKey]bool
	if rows, err := readRunsReadOnly(rowsPath); err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	} else if done, err = completed(rows, o.agentName, o.agent, o.model, exps); err != nil {
		return fmt.Errorf("%w: %s: %w", errUsage, rowsPath, err)
	}
	jobs := pending(exps, o.repeats, done)
	turns := 0
	var b strings.Builder
	for _, j := range jobs {
		turns += j.exp.TurnCap
		fmt.Fprintf(&b, "%s run %d (%s, turn cap %d)\n", j.exp.Package, j.run, j.exp.Tier, j.exp.TurnCap)
	}
	fmt.Fprintf(&b, "%d runs pending, %d already recorded in %s; at most %d agent turns (requests)\n",
		len(jobs), len(exps)*o.repeats-len(jobs), rowsPath, turns)
	_, err := io.WriteString(w, b.String())
	return err
}

// readRunsReadOnly reads the rows file like loadRuns but never modifies
// it.
func readRunsReadOnly(path string) ([]RunRow, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return parseRuns(path, data)
}

// astimateCommit returns the commit of the astimate checkout the runner
// runs from, or "" outside one.
func astimateCommit(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
