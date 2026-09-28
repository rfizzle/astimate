// Package runner carries out SPEC.md 11.2 steps 2 and 3 of the rebuild
// experiment: it hands each experiment to an agent in a fresh clone, runs
// the oracle, and appends one row per run to runs.jsonl
// (calibration/rebuild/README.md "The runner").
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rfizzle/astimate/calibration/internal/gocache"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/agent"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/pin"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/stub"
)

// Config are a Runner's settings.
type Config struct {
	// Def is the rebuild experiment definition.
	Def *definition.Definition
	// Out is the output directory for runs.jsonl and its transcripts.
	Out string
	// AgentName, Template and Model describe the agent invocation.
	AgentName string
	Template  string
	Model     string
	// Timeout is the wall-clock limit of one agent invocation.
	Timeout time.Duration
	// Keep, when true, keeps each run's clone instead of removing it,
	// together with the run's Go build cache, which lives beside the clone.
	Keep bool
	// Transcripts, when true, keeps each agent's standard output under
	// Out/transcripts.
	Transcripts bool
	// GoVersion is the toolchain the oracle runs with.
	GoVersion string
	// Clone fetches a module at its pin; pin.CloneAt outside tests.
	Clone func(ctx context.Context, repo, commit, dir string) error
	// Logger receives the runner's progress.
	Logger *slog.Logger
}

// Runner runs rebuild experiments and appends their rows.
type Runner struct {
	Config

	mu   sync.Mutex
	rows *os.File
}

// New returns a Runner configured by cfg.
func New(cfg Config) *Runner {
	return &Runner{Config: cfg}
}

// setupError marks a run that failed before the oracle could give a
// verdict; it writes no row, so a later invocation retries it.
func setupError(format string, args ...any) error {
	return fmt.Errorf("run setup failed: "+format, args...)
}

// UsageError is returned by Resume when the run cannot start: the runs
// file is unreadable, or it holds rows from another agent, model,
// template or stub.
type UsageError struct{ err error }

// Error returns the message, prefixed as a usage error.
func (e *UsageError) Error() string { return "usage: " + e.err.Error() }

// Unwrap returns the cause.
func (e *UsageError) Unwrap() error { return e.err }

// ReadFile reads path, returning nil, nil when it does not exist, as a
// runs file that no invocation has written yet.
func ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return data, nil
}

// loadRuns reads the rows of the runs file at path, first truncating a
// final line cut short by an interrupted write to the last complete line.
// A missing file is no rows.
func loadRuns(path string) ([]RunRow, error) {
	data, err := ReadFile(path)
	if err != nil || data == nil {
		return nil, err
	}
	if n := bytes.LastIndexByte(data, '\n') + 1; n < len(data) {
		if err := os.Truncate(path, int64(n)); err != nil {
			return nil, fmt.Errorf("dropping the partial last line of %s: %w", path, err)
		}
	}
	return ParseRuns(path, data)
}

// ParseRuns decodes the complete lines of a runs file's contents; a final
// line without a newline is an interrupted write and is ignored.
func ParseRuns(path string, data []byte) ([]RunRow, error) {
	data = data[:bytes.LastIndexByte(data, '\n')+1]
	var rows []RunRow
	line := 0
	for l := range bytes.Lines(data) {
		line++
		if len(bytes.TrimSpace(l)) == 0 {
			continue
		}
		var r RunRow
		if err := json.Unmarshal(l, &r); err != nil {
			return nil, fmt.Errorf("reading %s:%d: %w", path, line, err)
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// Completed returns the resume keys of rows whose oracle completed. It
// fails when a row was made by another agent, model or stub, or for
// another unit than unit, which would mix incomparable runs in one file.
func Completed(rows []RunRow, unit, agentName, template, model string, exps []definition.Experiment) (map[RunKey]bool, error) {
	stubs := make(map[string]string, len(exps))
	for _, e := range exps {
		stubs[e.Package] = e.StubSHA256
	}
	done := make(map[RunKey]bool, len(rows))
	for i := range rows {
		r := &rows[i]
		if u := RowUnit(r); u != unit {
			return nil, fmt.Errorf("%s run %d is a %s run, the definition's unit is %s; use another --out", r.Package, r.Run, u, unit)
		}
		if r.Agent.Name != agentName || r.Agent.Template != template || r.Agent.Model != model {
			return nil, fmt.Errorf("%s run %d was made by agent %q with model %q and template %q; "+
				"use another --out for a different agent", r.Package, r.Run, r.Agent.Name, r.Agent.Model, r.Agent.Template)
		}
		if h, ok := stubs[r.Package]; ok && h != r.StubSHA256 {
			return nil, fmt.Errorf("%s run %d started from stub %s, the definition's is %s", r.Package, r.Run, r.StubSHA256, h)
		}
		if r.Oracle.Completed {
			done[RunKey{r.Package, r.Run}] = true
		}
	}
	return done, nil
}

// Job is one pending run.
type Job struct {
	// Exp is the experiment to run.
	Exp *definition.Experiment
	// Run is the 1-based run index.
	Run int
}

// Pending returns the runs of exps not in done, run-major: every
// experiment's first run before any second run, so a partial result
// covers every experiment.
func Pending(exps []definition.Experiment, repeats int, done map[RunKey]bool) []Job {
	var jobs []Job
	for r := 1; r <= repeats; r++ {
		for i := range exps {
			if !done[RunKey{exps[i].Package, r}] {
				jobs = append(jobs, Job{&exps[i], r})
			}
		}
	}
	return jobs
}

// ResumeResult is the outcome of Runner.Resume.
type ResumeResult struct {
	// Skipped and Written are the runs already recorded, and the rows
	// added.
	Skipped, Written int
	// Failures are the runs that wrote no row.
	Failures []string
	// Rows are every row of the runs file afterwards.
	Rows []RunRow
}

// Resume runs every run of exps, repeats times each, that the runs file in
// rn's output directory does not already hold with a completed oracle,
// appending a row per run, and returns what it did. An error means
// nothing ran: the runs file is unreadable or holds another agent's runs.
func (rn *Runner) Resume(ctx context.Context, exps []definition.Experiment, repeats, parallel int) (ResumeResult, error) {
	var res ResumeResult
	path := filepath.Join(rn.Out, RunsFile)
	existing, err := loadRuns(path)
	if err != nil {
		return res, &UsageError{err: err}
	}
	done, err := Completed(existing, rn.Def.UnitOrDefault(), rn.AgentName, rn.Template, rn.Model, exps)
	if err != nil {
		return res, &UsageError{err: fmt.Errorf("%s: %w", path, err)}
	}
	jobs := Pending(exps, repeats, done)
	res.Skipped = len(exps)*repeats - len(jobs)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return res, &UsageError{err: err}
	}
	defer func() { _ = f.Close() }()
	rn.rows = f
	rn.Logger.Info("running", "experiments", len(exps), "repeats", repeats, "pending", len(jobs),
		"skipped", res.Skipped, "out", rn.Out)
	res.Written, res.Failures = rn.runJobs(ctx, jobs, parallel)
	if res.Failures == nil {
		res.Failures = []string{}
	}
	res.Rows, err = loadRuns(path)
	return res, err
}

// runJobs runs jobs with at most parallel at a time and returns how many
// rows were written and the setup failures. It stops starting runs when
// ctx is canceled.
func (rn *Runner) runJobs(ctx context.Context, jobs []Job, parallel int) (int, []string) {
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		written  int
		failures []string
	)
	// streak counts failures since the last recorded run; at maxStreak
	// the setup is broken for every run (no network, no login), and no
	// further run starts.
	const maxStreak = 3
	streak := 0
	stopped := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return streak >= maxStreak
	}
	sem := make(chan struct{}, max(parallel, 1))
	for _, j := range jobs {
		select {
		case <-ctx.Done():
		case sem <- struct{}{}:
		}
		if ctx.Err() != nil || stopped() {
			break
		}
		wg.Go(func() {
			defer func() { <-sem }()
			log := rn.Logger.With("package", j.Exp.Package, "run", j.Run)
			log.Info("run started")
			row, err := rn.runOne(ctx, j.Exp, j.Run)
			if err == nil {
				err = rn.appendRow(row)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				log.Error("run failed", "err", err)
				failures = append(failures, fmt.Sprintf("%s run %d: %v", j.Exp.Package, j.Run, err))
				if streak++; streak == maxStreak {
					rn.Logger.Error("stopping: runs keep failing before the oracle; fix the cause and rerun to resume",
						"failures", maxStreak)
				}
				return
			}
			streak = 0
			written++
			log.Info("run recorded", "passed", row.Oracle.Passed, "valid", row.Valid,
				"turns", optString(row.Measured.Turns), "wall", time.Duration(row.Agent.WallMS)*time.Millisecond)
		})
	}
	wg.Wait()
	slices.Sort(failures)
	return written, failures
}

// appendRow writes row as one line of runs.jsonl and syncs it.
func (rn *Runner) appendRow(row RunRow) error {
	data, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("encoding row: %w", err)
	}
	rn.mu.Lock()
	defer rn.mu.Unlock()
	if _, err := rn.rows.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing %s: %w", RunsFile, err)
	}
	if err := rn.rows.Sync(); err != nil {
		return fmt.Errorf("syncing %s: %w", RunsFile, err)
	}
	return nil
}

// prepareGo runs a go command in root that must pass before the agent
// starts, as a setup error naming failMsg on a plain failure, or naming the
// interruption when ctx was canceled first.
func (rn *Runner) prepareGo(ctx context.Context, root string, env []string, failMsg string, args ...string) error {
	out, err := pin.RunGo(ctx, root, env, args...)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return setupError("interrupted before the agent: %w", ctx.Err())
	}
	return setupError("%s: %s", failMsg, pin.Tail(out))
}

// runEnv returns the experiment's environment def for one run in the
// directory work, with GOCACHE and GOTMPDIR pointed under work: every
// build of the run (the pre-build, the agent's and the oracle's) then
// stays out of the shared Go build cache and is removed with the clone.
func runEnv(def []string, work string) ([]string, error) {
	cache, err := gocache.Env(work)
	if err != nil {
		return nil, err
	}
	return append(slices.Clip(def), cache...), nil
}

// runOne runs experiment e once in a fresh clone and returns its row. An
// error means no verdict: the clone, stub or pre-check failed, or ctx was
// canceled.
func (rn *Runner) runOne(ctx context.Context, e *definition.Experiment, run int) (RunRow, error) {
	row := RunRow{
		Schema: rowSchema, Module: e.Module, Package: e.Package, Dir: e.Dir, Commit: e.Commit,
		StubSHA256: e.StubSHA256, Run: run, TurnCap: e.TurnCap, Unit: rn.Def.UnitOrDefault(),
		Estimate: Estimate{Tier: e.Tier, AgentPasses: e.AgentPasses, RebuildTokens: e.RebuildTokens,
			HumanDays: e.HumanDays, HasTests: e.HasTests},
		Metrics:       e.Metrics.RawMetrics,
		Members:       members(e),
		ConfigVersion: rn.Def.ConfigVersion,
		GoVersion:     rn.GoVersion,
		StartedAt:     time.Now().UTC(),
	}
	work, err := os.MkdirTemp("", "astimate-rebuild-run-")
	if err != nil {
		return row, setupError("%w", err)
	}
	if rn.Keep {
		rn.Logger.Info("keeping clone and its build cache", "package", e.Package, "run", run, "dir", work)
	} else {
		defer func() { _ = os.RemoveAll(work) }()
	}
	env, err := runEnv(rn.Def.Env, work)
	if err != nil {
		return row, setupError("%w", err)
	}
	root := filepath.Join(work, "module")
	if err := rn.Clone(ctx, e.Repo, e.Commit, root); err != nil {
		return row, setupError("cloning: %w", err)
	}
	summary, err := stub.Apply(ctx, root, e, env)
	if err != nil {
		return row, setupError("%w", err)
	}
	// Download and compile everything the oracle needs before the agent
	// starts, so its wall time holds no module downloads, and prove the
	// stub builds with its tests.
	if err := rn.prepareGo(ctx, root, env, "stubbed module does not build", "build", "./..."); err != nil {
		return row, err
	}
	warmArgs := append([]string{"test", "-count=1", "-run", "^$"}, e.Oracle.Test...)
	if err := rn.prepareGo(ctx, root, env, "oracle tests do not build on the stub", warmArgs...); err != nil {
		return row, err
	}
	promptFile := filepath.Join(work, "prompt.txt")
	if err := os.WriteFile(promptFile, []byte(agent.BuildPrompt(e, root, rn.Def.Env, summary)), 0o644); err != nil {
		return row, setupError("%w", err)
	}
	row.Agent = AgentRun{Name: rn.AgentName, Template: rn.Template, Model: rn.Model}
	row.Agent.Command = agent.RenderTemplate(rn.Template, map[string]string{
		"root": root, "dir": e.Dir, "package": e.Package, "prompt_file": promptFile,
		"turn_cap": strconv.Itoa(e.TurnCap), "model": rn.Model,
	})
	stdout, err := rn.invoke(ctx, root, env, e, run, &row.Agent)
	if err != nil {
		return row, err
	}
	row.Measured = agent.ParseAgentOutput(stdout, e.TurnCap)
	// An agent that exits with an error and no result, before its time
	// was up, never ran a session: a bad flag, no login, no claude on
	// PATH. Its run is retried rather than recorded as a failed rebuild.
	if row.Measured.ResultSubtype == nil && row.Agent.ExitCode != 0 && !row.Agent.TimedOut {
		return row, setupError("agent exited with status %d and no result: %s",
			row.Agent.ExitCode, tailLine(row.Agent.StderrTail))
	}
	if err := rn.oracle(ctx, root, env, e, &row.Oracle); err != nil {
		return row, err
	}
	row.Changes, err = changes(ctx, root, packageDirs(e))
	if err != nil {
		return row, setupError("%w", err)
	}
	row.Valid = len(row.Changes.TestFiles) == 0 && len(row.Changes.OutsidePackage) == 0
	row.FinishedAt = time.Now().UTC()
	return row, nil
}

// invoke runs the agent command in root through sh -c with the run's
// environment env and the wall-clock timeout, records how it
// ended in a, and returns its standard output. It fails only when the
// command could not run for a reason outside the agent: ctx canceled or
// the transcript not writable.
func (rn *Runner) invoke(ctx context.Context, root string, env []string, e *definition.Experiment, run int, a *AgentRun) ([]byte, error) {
	actx, cancel := context.WithTimeout(ctx, rn.Timeout)
	defer cancel()
	cmd := exec.CommandContext(actx, "sh", "-c", a.Command)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = nil
	cmd.WaitDelay = 10 * time.Second
	killGroup(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &tailBuffer{max: 4096, buf: &stderr}
	var transcript *os.File
	if rn.Transcripts {
		dir := filepath.Join(rn.Out, "transcripts")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, setupError("%w", err)
		}
		name := strconv.Itoa(run) + "-" + strings.NewReplacer("/", "_", ".", "_").Replace(e.Package)
		f, err := os.Create(filepath.Join(dir, name+".jsonl"))
		if err != nil {
			return nil, setupError("%w", err)
		}
		transcript = f
		defer func() { _ = transcript.Close() }()
		cmd.Stdout = io.MultiWriter(&stdout, transcript)
	}
	start := time.Now()
	err := cmd.Run()
	a.WallMS = time.Since(start).Milliseconds()
	if ctx.Err() != nil {
		return nil, setupError("interrupted: %w", ctx.Err())
	}
	a.TimedOut = errors.Is(actx.Err(), context.DeadlineExceeded)
	a.ExitCode = 0
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		a.ExitCode = exit.ExitCode()
	case err != nil:
		a.ExitCode = -1
		a.StderrTail = err.Error()
	}
	if a.StderrTail == "" {
		a.StderrTail = strings.TrimSpace(stderr.String())
	}
	return stdout.Bytes(), nil
}

// oracle runs the experiment's oracle in root with the run's environment
// env and records the outcome in o. It fails only when ctx was canceled,
// which leaves no verdict.
func (rn *Runner) oracle(ctx context.Context, root string, env []string, e *definition.Experiment, o *OracleOutcome) error {
	start := time.Now()
	o.Test, o.Build = e.Oracle.Test, e.Oracle.Build
	out, err := pin.RunGo(ctx, root, env, append([]string{"test", "-count=1"}, e.Oracle.Test...)...)
	o.TestsPass, o.TestTail = err == nil, pin.Tail(out)
	out, err = pin.RunGo(ctx, root, env, append([]string{"build"}, e.Oracle.Build...)...)
	o.BuildPasses, o.BuildTail = err == nil, pin.Tail(out)
	if ctx.Err() != nil {
		return setupError("interrupted during the oracle: %w", ctx.Err())
	}
	o.Passed = o.TestsPass && o.BuildPasses
	o.WallMS = time.Since(start).Milliseconds()
	o.Completed = true
	return nil
}

// packageDirs returns the module-relative directories whose non-test files
// the agent may change: the package's, or every member's of a tree.
func packageDirs(e *definition.Experiment) []string {
	if len(e.Members) == 0 {
		return []string{e.Dir}
	}
	dirs := make([]string, len(e.Members))
	for i, m := range e.Members {
		dirs[i] = m.Dir
	}
	return dirs
}

// changes lists the changed paths in the clone at root that the prompt
// ruled out: test files anywhere, and anything not directly in one of
// dirs. For a tree that includes a new package below it and any main
// package below it, which the stub did not touch.
func changes(ctx context.Context, root string, dirs []string) (Changes, error) {
	c := Changes{TestFiles: []string{}, OutsidePackage: []string{}}
	out, err := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain=v1", "-z", "--untracked-files=all").Output()
	if err != nil {
		return c, fmt.Errorf("git status: %w", err)
	}
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		status, path := f[:2], f[3:]
		paths := []string{path}
		if status[0] == 'R' || status[0] == 'C' {
			// A rename's source follows as the next field.
			i++
			if i < len(fields) {
				paths = append(paths, fields[i])
			}
		}
		for _, p := range paths {
			switch {
			case strings.HasSuffix(p, "_test.go"):
				c.TestFiles = append(c.TestFiles, p)
			case !slices.Contains(dirs, filepath.ToSlash(filepath.Dir(p))):
				c.OutsidePackage = append(c.OutsidePackage, p)
			}
		}
	}
	slices.Sort(c.TestFiles)
	slices.Sort(c.OutsidePackage)
	return c, nil
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	max int
	buf *bytes.Buffer
}

// Write appends p, dropping the oldest bytes beyond max.
func (t *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > t.max {
		p = p[len(p)-t.max:]
	}
	if over := t.buf.Len() + len(p) - t.max; over > 0 {
		t.buf.Next(over)
	}
	t.buf.Write(p)
	return n, nil
}

// optString formats an optional measurement for a log line, "null" when
// absent.
func optString[T any](p *T) string {
	if p == nil {
		return "null"
	}
	return fmt.Sprint(*p)
}

// tailLine returns the last non-empty line of s, for an error message.
func tailLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
