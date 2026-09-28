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

	"github.com/rfizzle/astimate/calibration/rebuild/internal/agent"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/pin"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/runner"
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
	fs.StringVar(&o.out, "out", "", "output directory for runs.jsonl and run.json (default calibration/data/rebuild-<date>-<agent name>, "+
		"or rebuild-trees-<date>-<agent name> for a tree definition)")
	fs.IntVar(&o.repeats, "repeats", 3, "runs per experiment")
	fs.StringVar(&o.only, "only", "", "run only the experiment of this package import path, or of this module")
	fs.StringVar(&o.agent, "agent", "", "agent command template run by sh -c in the module root, with placeholders {"+
		strings.Join(agent.Placeholders(), "}, {")+"} (default: Claude Code in print mode, which needs --live)")
	fs.StringVar(&o.agentName, "agent-name", "", "agent name for the rows and the default output directory (default "+
		agent.DefaultName+", or "+agent.CustomName+" with --agent)")
	fs.StringVar(&o.model, "model", agent.DefaultModel, "model the agent is asked to use ({model})")
	fs.BoolVar(&o.live, "live", false, "allow an agent command that runs Claude Code (the default one does): this spends live requests")
	fs.DurationVar(&o.timeout, "timeout", 60*time.Minute, "wall-clock limit of one agent invocation")
	fs.IntVar(&o.parallel, "parallel", 1, "runs at a time")
	fs.BoolVar(&o.keep, "keep", false, "keep each run's clone and its Go build cache instead of removing them")
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
		o.agent = agent.DefaultTemplate
	}
	if err := agent.CheckTemplate(o.agent); err != nil {
		return o, fmt.Errorf("%w: %w", errUsage, err)
	}
	if o.agentName == "" {
		o.agentName = agent.DefaultName
		if o.agentSet {
			o.agentName = agent.CustomName
		}
	}
	return o, nil
}

// defaultOut is the output directory of a run of a definition with unit
// when --out is not given: calibration/data/rebuild-<date>-<agent name>,
// with rebuild-trees- for trees, so tree and package rows never share a
// file.
func defaultOut(unit, agentName string, now time.Time) string {
	prefix := "rebuild-"
	if unit == definition.UnitTree {
		prefix = "rebuild-trees-"
	}
	return filepath.Join("calibration", "data", prefix+now.Format(time.DateOnly)+"-"+agentName)
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

// selectExperiments returns the experiments matching only: a package
// import path or a module path; all of them when only is empty.
func selectExperiments(exps []definition.Experiment, only string) ([]definition.Experiment, error) {
	if only == "" {
		return exps, nil
	}
	var out []definition.Experiment
	for _, e := range exps {
		if e.Package == only || e.Module == only {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--only %s matches no experiment's package or module", only)
	}
	return out, nil
}

// runRuns runs the rebuild experiments.
func runRuns(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	o, err := parseRunFlags(args, stderr)
	if err != nil {
		return err
	}
	def, err := definition.LoadDefinition(o.definition)
	if err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	exps, err := selectExperiments(def.Experiments, o.only)
	if err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if o.out == "" {
		o.out = defaultOut(def.UnitOrDefault(), o.agentName, time.Now())
	}
	if o.plan {
		return printPlan(stdout, filepath.Join(o.out, runner.RunsFile), def.UnitOrDefault(), exps, o)
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
	gover, err := pin.GoVersion(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	rn := runner.New(runner.Config{
		Def: def, Out: o.out, AgentName: o.agentName, Template: o.agent, Model: o.model,
		Timeout: o.timeout, Keep: o.keep, Transcripts: o.transcripts, GoVersion: gover,
		Clone: pin.CloneAt, Logger: logger,
	})
	info := runner.RunInfo{
		StartedAt: time.Now().UTC(), GoVersion: gover, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, CPUs: runtime.NumCPU(),
		AstimateCommit: astimateCommit(ctx), Definition: filepath.ToSlash(o.definition),
		DefinitionConfigVersion: def.ConfigVersion, DefinitionGoVersion: def.GoVersion, Unit: def.UnitOrDefault(), Env: def.Env,
		Agent: o.agentName, Template: o.agent, Model: o.model, Repeats: o.repeats, Parallel: o.parallel,
		TimeoutSeconds: o.timeout.Seconds(), Only: o.only, Experiments: len(exps),
	}
	res, err := rn.Resume(ctx, exps, o.repeats, o.parallel)
	if err != nil {
		var uerr *runner.UsageError
		if errors.As(err, &uerr) {
			return fmt.Errorf("%w: %w", errUsage, uerr.Unwrap())
		}
		return err
	}
	info.Skipped, info.Written, info.Failures = res.Skipped, res.Written, res.Failures
	info.FinishedAt = time.Now().UTC()
	info.Totals = runner.SumTotals(res.Rows)
	if err := runner.WriteJSON(filepath.Join(o.out, runner.RunInfoFile), info); err != nil {
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
func printPlan(w io.Writer, rowsPath, unit string, exps []definition.Experiment, o runOptions) error {
	var done map[runner.RunKey]bool
	if rows, err := readRunsReadOnly(rowsPath); err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	} else if done, err = runner.Completed(rows, unit, o.agentName, o.agent, o.model, exps); err != nil {
		return fmt.Errorf("%w: %s: %w", errUsage, rowsPath, err)
	}
	jobs := runner.Pending(exps, o.repeats, done)
	turns := 0
	var b strings.Builder
	for _, j := range jobs {
		turns += j.Exp.TurnCap
		fmt.Fprintf(&b, "%s run %d (%s, turn cap %d)\n", j.Exp.Package, j.Run, j.Exp.Tier, j.Exp.TurnCap)
	}
	fmt.Fprintf(&b, "%d runs pending, %d already recorded in %s; at most %d agent turns (requests)\n",
		len(jobs), len(exps)*o.repeats-len(jobs), rowsPath, turns)
	_, err := io.WriteString(w, b.String())
	return err
}

// readRunsReadOnly reads the rows file like the runner's own resume, but
// never modifies it.
func readRunsReadOnly(path string) ([]runner.RunRow, error) {
	data, err := runner.ReadFile(path)
	if err != nil || data == nil {
		return nil, err
	}
	return runner.ParseRuns(path, data)
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
