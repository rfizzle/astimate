package main

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
	"strings"
	"sync"
	"time"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// rowSchema is the version of the RunRow layout; it changes when a field
// is renamed or removed.
const rowSchema = 1

// runsFile and runInfoFile are the runner's outputs in its directory.
const (
	runsFile    = "runs.jsonl"
	runInfoFile = "run.json"
)

// RunRow is one line of runs.jsonl: one agent run of one experiment, with
// everything the fit regresses on, so it never has to reload the corpus
// data. The layout is documented in calibration/rebuild/README.md.
type RunRow struct {
	// Schema is rowSchema.
	Schema int `json:"schema"`
	// Module, Package and Dir identify the experiment.
	Module  string `json:"module"`
	Package string `json:"package"`
	Dir     string `json:"dir"`
	// Commit is the pin the module was cloned at.
	Commit string `json:"commit"`
	// StubSHA256 is the tree hash of the stub the run started from.
	StubSHA256 string `json:"stub_sha256"`
	// Run is the 1-based run index; (Package, Run) is the resume key.
	Run int `json:"run"`
	// TurnCap is the experiment's turn cap.
	TurnCap int `json:"turn_cap"`
	// Agent is the invocation and how it ended.
	Agent AgentRun `json:"agent"`
	// Estimate is the estimate before the run, from the definition.
	Estimate Estimate `json:"estimate"`
	// Metrics are the package's raw metrics at the pin, the section 7.1
	// inputs among them.
	Metrics metrics.RawMetrics `json:"metrics"`
	// ConfigVersion is the configuration the estimate was made under.
	ConfigVersion string `json:"config_version"`
	// GoVersion is the toolchain the oracle ran with.
	GoVersion string `json:"go_version"`
	// Measured is what the agent reported about the session.
	Measured Measured `json:"measured"`
	// Oracle is the oracle's outcome after the agent finished.
	Oracle OracleOutcome `json:"oracle"`
	// Changes lists the files the agent should not have touched.
	Changes Changes `json:"changes"`
	// Valid is true when the agent changed no test file and nothing
	// outside the package, so the oracle's verdict stands.
	Valid bool `json:"valid"`
	// StartedAt and FinishedAt bound the run, the clone and oracle
	// included, in UTC.
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

// AgentRun is the agent invocation of a run.
type AgentRun struct {
	// Name is the agent's name (claude-code for the default).
	Name string `json:"name"`
	// Template is the command template before substitution.
	Template string `json:"template"`
	// Command is the command run through sh -c.
	Command string `json:"command"`
	// Model is the model asked for.
	Model string `json:"model"`
	// ExitCode is the command's exit status; -1 when it was killed.
	ExitCode int `json:"exit_code"`
	// TimedOut is true when the wall-clock timeout killed the command.
	TimedOut bool `json:"timed_out"`
	// WallMS is the command's wall time, measured by the runner.
	WallMS int64 `json:"wall_ms"`
	// StderrTail is the end of the command's standard error.
	StderrTail string `json:"stderr_tail"`
}

// Estimate is the pre-run estimate of an experiment.
type Estimate struct {
	// Tier is the SPEC.md 7.4 tier.
	Tier score.Tier `json:"tier"`
	// AgentPasses, RebuildTokens and HumanDays are the estimate's outputs.
	AgentPasses   float64 `json:"agent_passes"`
	RebuildTokens int     `json:"rebuild_tokens"`
	HumanDays     float64 `json:"human_days"`
	// HasTests says whether the oracle is the package's own tests.
	HasTests bool `json:"has_tests"`
}

// OracleOutcome is the oracle's verdict on a run.
type OracleOutcome struct {
	// Test and Build are the patterns the oracle ran.
	Test  []string `json:"test"`
	Build []string `json:"build"`
	// TestsPass is true when go test of Test passed.
	TestsPass bool `json:"tests_pass"`
	// BuildPasses is true when go build of Build passed: importers still
	// compile.
	BuildPasses bool `json:"build_passes"`
	// Passed is TestsPass and BuildPasses.
	Passed bool `json:"passed"`
	// TestTail and BuildTail are the end of each command's output.
	TestTail  string `json:"test_tail"`
	BuildTail string `json:"build_tail"`
	// WallMS is the oracle's wall time.
	WallMS int64 `json:"wall_ms"`
	// Completed is true when both commands ran to an outcome; resume skips
	// only completed runs.
	Completed bool `json:"completed"`
}

// Changes are the working-tree changes after the agent that break the
// rules of the prompt.
type Changes struct {
	// TestFiles are the _test.go files changed, added or removed.
	TestFiles []string `json:"test_files"`
	// OutsidePackage are the other paths changed, added or removed outside
	// the package directory.
	OutsidePackage []string `json:"outside_package"`
}

// runKey is the resume key of a run.
type runKey struct {
	pkg string
	run int
}

// runner runs rebuild experiments and appends their rows.
type runner struct {
	def       *Definition
	out       string
	agentName string
	template  string
	model     string
	timeout   time.Duration
	keep      bool
	// transcripts keeps each agent's standard output under out.
	transcripts bool
	goVersion   string
	// clone fetches a module at its pin; CloneAt outside tests.
	clone  func(ctx context.Context, repo, commit, dir string) error
	logger *slog.Logger

	mu   sync.Mutex
	rows *os.File
}

// errSetup marks a run that failed before the oracle could give a verdict;
// it writes no row, so a later invocation retries it.
var errSetup = errors.New("run setup failed")

// loadRuns reads the rows of the runs file at path, first truncating a
// final line cut short by an interrupted write to the last complete line.
// A missing file is no rows.
func loadRuns(path string) ([]RunRow, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if n := bytes.LastIndexByte(data, '\n') + 1; n < len(data) {
		if err := os.Truncate(path, int64(n)); err != nil {
			return nil, fmt.Errorf("dropping the partial last line of %s: %w", path, err)
		}
	}
	return parseRuns(path, data)
}

// parseRuns decodes the complete lines of a runs file's contents; a final
// line without a newline is an interrupted write and is ignored.
func parseRuns(path string, data []byte) ([]RunRow, error) {
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

// completed returns the resume keys of rows whose oracle completed. It
// fails when a row was made by another agent, model or stub, which would
// mix incomparable runs in one file.
func completed(rows []RunRow, agentName, template, model string, exps []Experiment) (map[runKey]bool, error) {
	stubs := make(map[string]string, len(exps))
	for _, e := range exps {
		stubs[e.Package] = e.StubSHA256
	}
	done := make(map[runKey]bool, len(rows))
	for _, r := range rows {
		if r.Agent.Name != agentName || r.Agent.Template != template || r.Agent.Model != model {
			return nil, fmt.Errorf("%s run %d was made by agent %q with model %q and template %q; "+
				"use another --out for a different agent", r.Package, r.Run, r.Agent.Name, r.Agent.Model, r.Agent.Template)
		}
		if h, ok := stubs[r.Package]; ok && h != r.StubSHA256 {
			return nil, fmt.Errorf("%s run %d started from stub %s, the definition's is %s", r.Package, r.Run, r.StubSHA256, h)
		}
		if r.Oracle.Completed {
			done[runKey{r.Package, r.Run}] = true
		}
	}
	return done, nil
}

// job is one pending run.
type job struct {
	exp *Experiment
	run int
}

// pending returns the runs of exps not in done, run-major: every
// experiment's first run before any second run, so a partial result
// covers every experiment.
func pending(exps []Experiment, repeats int, done map[runKey]bool) []job {
	var jobs []job
	for r := 1; r <= repeats; r++ {
		for i := range exps {
			if !done[runKey{exps[i].Package, r}] {
				jobs = append(jobs, job{&exps[i], r})
			}
		}
	}
	return jobs
}

// resumeResult is the outcome of runner.resume.
type resumeResult struct {
	// skipped are the runs already recorded, written the rows added.
	skipped, written int
	// failures are the runs that wrote no row.
	failures []string
	// rows are every row of the runs file afterwards.
	rows []RunRow
}

// resume runs every run of exps, repeats times each, that the runs file
// in rn.out does not already hold with a completed oracle, appending a row
// per run, and returns what it did. An error means nothing ran: the runs
// file is unreadable or holds another agent's runs.
func (rn *runner) resume(ctx context.Context, exps []Experiment, repeats, parallel int) (resumeResult, error) {
	var res resumeResult
	path := filepath.Join(rn.out, runsFile)
	existing, err := loadRuns(path)
	if err != nil {
		return res, fmt.Errorf("%w: %w", errUsage, err)
	}
	done, err := completed(existing, rn.agentName, rn.template, rn.model, exps)
	if err != nil {
		return res, fmt.Errorf("%w: %s: %w", errUsage, path, err)
	}
	jobs := pending(exps, repeats, done)
	res.skipped = len(exps)*repeats - len(jobs)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return res, fmt.Errorf("%w: %w", errUsage, err)
	}
	defer func() { _ = f.Close() }()
	rn.rows = f
	rn.logger.Info("running", "experiments", len(exps), "repeats", repeats, "pending", len(jobs),
		"skipped", res.skipped, "out", rn.out)
	res.written, res.failures = rn.runJobs(ctx, jobs, parallel)
	if res.failures == nil {
		res.failures = []string{}
	}
	res.rows, err = loadRuns(path)
	return res, err
}

// runJobs runs jobs with at most parallel at a time and returns how many
// rows were written and the setup failures. It stops starting runs when
// ctx is canceled.
func (rn *runner) runJobs(ctx context.Context, jobs []job, parallel int) (int, []string) {
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
			log := rn.logger.With("package", j.exp.Package, "run", j.run)
			log.Info("run started")
			row, err := rn.runOne(ctx, j.exp, j.run)
			if err == nil {
				err = rn.appendRow(row)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				log.Error("run failed", "err", err)
				failures = append(failures, fmt.Sprintf("%s run %d: %v", j.exp.Package, j.run, err))
				if streak++; streak == maxStreak {
					rn.logger.Error("stopping: runs keep failing before the oracle; fix the cause and rerun to resume",
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
func (rn *runner) appendRow(row RunRow) error {
	data, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("encoding row: %w", err)
	}
	rn.mu.Lock()
	defer rn.mu.Unlock()
	if _, err := rn.rows.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing %s: %w", runsFile, err)
	}
	if err := rn.rows.Sync(); err != nil {
		return fmt.Errorf("syncing %s: %w", runsFile, err)
	}
	return nil
}

// runOne runs experiment e once in a fresh clone and returns its row. An
// error means no verdict: the clone, stub or pre-check failed, or ctx was
// canceled.
func (rn *runner) runOne(ctx context.Context, e *Experiment, run int) (RunRow, error) {
	row := RunRow{
		Schema: rowSchema, Module: e.Module, Package: e.Package, Dir: e.Dir, Commit: e.Commit,
		StubSHA256: e.StubSHA256, Run: run, TurnCap: e.TurnCap,
		Estimate: Estimate{Tier: e.Tier, AgentPasses: e.AgentPasses, RebuildTokens: e.RebuildTokens,
			HumanDays: e.HumanDays, HasTests: e.HasTests},
		Metrics:       e.Metrics.RawMetrics,
		ConfigVersion: rn.def.ConfigVersion,
		GoVersion:     rn.goVersion,
		StartedAt:     time.Now().UTC(),
	}
	work, err := os.MkdirTemp("", "astimate-rebuild-run-")
	if err != nil {
		return row, fmt.Errorf("%w: %w", errSetup, err)
	}
	if rn.keep {
		rn.logger.Info("keeping clone", "package", e.Package, "run", run, "dir", work)
	} else {
		defer func() { _ = os.RemoveAll(work) }()
	}
	root := filepath.Join(work, "module")
	if err := rn.clone(ctx, e.Repo, e.Commit, root); err != nil {
		return row, fmt.Errorf("%w: cloning: %w", errSetup, err)
	}
	if err := ApplyStub(root, e); err != nil {
		return row, fmt.Errorf("%w: %w", errSetup, err)
	}
	// Download and compile everything the oracle needs before the agent
	// starts, so its wall time holds no module downloads, and prove the
	// stub builds with its tests.
	if out, err := runGo(ctx, root, rn.def.Env, "build", "./..."); err != nil {
		if ctx.Err() != nil {
			return row, fmt.Errorf("%w: interrupted before the agent: %w", errSetup, ctx.Err())
		}
		return row, fmt.Errorf("%w: stubbed module does not build: %s", errSetup, tail(out))
	}
	warmArgs := append([]string{"test", "-count=1", "-run", "^$"}, e.Oracle.Test...)
	if out, err := runGo(ctx, root, rn.def.Env, warmArgs...); err != nil {
		if ctx.Err() != nil {
			return row, fmt.Errorf("%w: interrupted before the agent: %w", errSetup, ctx.Err())
		}
		return row, fmt.Errorf("%w: oracle tests do not build on the stub: %s", errSetup, tail(out))
	}
	promptFile := filepath.Join(work, "prompt.txt")
	if err := os.WriteFile(promptFile, []byte(buildPrompt(e, root, rn.def.Env)), 0o644); err != nil {
		return row, fmt.Errorf("%w: %w", errSetup, err)
	}
	row.Agent = AgentRun{Name: rn.agentName, Template: rn.template, Model: rn.model}
	row.Agent.Command = renderTemplate(rn.template, map[string]string{
		"root": root, "dir": e.Dir, "package": e.Package, "prompt_file": promptFile,
		"turn_cap": itoa(e.TurnCap), "model": rn.model,
	})
	stdout, err := rn.invoke(ctx, root, e, run, &row.Agent)
	if err != nil {
		return row, err
	}
	row.Measured = ParseAgentOutput(stdout, e.TurnCap)
	// An agent that exits with an error and no result, before its time
	// was up, never ran a session: a bad flag, no login, no claude on
	// PATH. Its run is retried rather than recorded as a failed rebuild.
	if row.Measured.ResultSubtype == nil && row.Agent.ExitCode != 0 && !row.Agent.TimedOut {
		return row, fmt.Errorf("%w: agent exited with status %d and no result: %s",
			errSetup, row.Agent.ExitCode, tailLine(row.Agent.StderrTail))
	}
	if err := rn.oracle(ctx, root, e, &row.Oracle); err != nil {
		return row, err
	}
	row.Changes, err = changes(ctx, root, e.Dir)
	if err != nil {
		return row, fmt.Errorf("%w: %w", errSetup, err)
	}
	row.Valid = len(row.Changes.TestFiles) == 0 && len(row.Changes.OutsidePackage) == 0
	row.FinishedAt = time.Now().UTC()
	return row, nil
}

// invoke runs the agent command in root through sh -c with the
// experiment's environment and the wall-clock timeout, records how it
// ended in a, and returns its standard output. It fails only when the
// command could not run for a reason outside the agent: ctx canceled or
// the transcript not writable.
func (rn *runner) invoke(ctx context.Context, root string, e *Experiment, run int, a *AgentRun) ([]byte, error) {
	actx, cancel := context.WithTimeout(ctx, rn.timeout)
	defer cancel()
	cmd := exec.CommandContext(actx, "sh", "-c", a.Command)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), rn.def.Env...)
	cmd.Stdin = nil
	cmd.WaitDelay = 10 * time.Second
	killGroup(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &tailBuffer{max: 4096, buf: &stderr}
	var transcript *os.File
	if rn.transcripts {
		dir := filepath.Join(rn.out, "transcripts")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("%w: %w", errSetup, err)
		}
		name := itoa(run) + "-" + strings.NewReplacer("/", "_", ".", "_").Replace(e.Package)
		f, err := os.Create(filepath.Join(dir, name+".jsonl"))
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errSetup, err)
		}
		transcript = f
		defer func() { _ = transcript.Close() }()
		cmd.Stdout = io.MultiWriter(&stdout, transcript)
	}
	start := time.Now()
	err := cmd.Run()
	a.WallMS = time.Since(start).Milliseconds()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%w: interrupted: %w", errSetup, ctx.Err())
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

// oracle runs the experiment's oracle in root and records the outcome in
// o. It fails only when ctx was canceled, which leaves no verdict.
func (rn *runner) oracle(ctx context.Context, root string, e *Experiment, o *OracleOutcome) error {
	start := time.Now()
	o.Test, o.Build = e.Oracle.Test, e.Oracle.Build
	out, err := runGo(ctx, root, rn.def.Env, append([]string{"test", "-count=1"}, e.Oracle.Test...)...)
	o.TestsPass, o.TestTail = err == nil, tail(out)
	out, err = runGo(ctx, root, rn.def.Env, append([]string{"build"}, e.Oracle.Build...)...)
	o.BuildPasses, o.BuildTail = err == nil, tail(out)
	if ctx.Err() != nil {
		return fmt.Errorf("%w: interrupted during the oracle: %w", errSetup, ctx.Err())
	}
	o.Passed = o.TestsPass && o.BuildPasses
	o.WallMS = time.Since(start).Milliseconds()
	o.Completed = true
	return nil
}

// changes lists the changed paths in the clone at root that the prompt
// ruled out: test files anywhere, and anything outside dir.
func changes(ctx context.Context, root, dir string) (Changes, error) {
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
			case !inPackage(dir, p):
				c.OutsidePackage = append(c.OutsidePackage, p)
			}
		}
	}
	slices.Sort(c.TestFiles)
	slices.Sort(c.OutsidePackage)
	return c, nil
}

// inPackage reports whether the module-relative path p is a file directly
// in the package directory dir; a file in a subdirectory belongs to
// another package.
func inPackage(dir, p string) bool {
	d := filepath.ToSlash(filepath.Dir(p))
	return d == dir
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

// RunInfo is run.json: the environment of the latest invocation and the
// totals over every row of runs.jsonl.
type RunInfo struct {
	// StartedAt and FinishedAt bound the latest invocation, in UTC.
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	// GoVersion, GOOS, GOARCH and CPUs describe the host.
	GoVersion string `json:"go_version"`
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
	CPUs      int    `json:"cpus"`
	// AstimateCommit is the astimate commit the runner ran from.
	AstimateCommit string `json:"astimate_commit"`
	// Definition is the definition file, with its config_version and the
	// go_version its stubs were verified with.
	Definition              string `json:"definition"`
	DefinitionConfigVersion string `json:"definition_config_version"`
	DefinitionGoVersion     string `json:"definition_go_version"`
	// Env is the environment of every go command and of the agent.
	Env []string `json:"env"`
	// Agent, Template and Model describe the agent invocation.
	Agent    string `json:"agent"`
	Template string `json:"template"`
	Model    string `json:"model"`
	// Repeats, Parallel, TimeoutSeconds and Only are the run's settings.
	Repeats        int     `json:"repeats"`
	Parallel       int     `json:"parallel"`
	TimeoutSeconds float64 `json:"timeout_seconds"`
	Only           string  `json:"only"`
	// Experiments is the number of experiments selected.
	Experiments int `json:"experiments"`
	// Skipped are the runs already in runs.jsonl at the start.
	Skipped int `json:"skipped"`
	// Written are the rows this invocation added.
	Written int `json:"written"`
	// Failures are the runs of this invocation that wrote no row.
	Failures []string `json:"failures"`
	// Totals sum every row of runs.jsonl.
	Totals Totals `json:"totals"`
}

// Totals sums the rows of runs.jsonl.
type Totals struct {
	// Rows is the number of rows; Passed and Valid count rows whose oracle
	// passed and rows that kept to the rules.
	Rows   int `json:"rows"`
	Passed int `json:"passed"`
	Valid  int `json:"valid"`
	// TurnCapHit counts the rows whose agent hit the turn cap.
	TurnCapHit int `json:"turn_cap_hit"`
	// InputTokens through CostUSD sum the measurements the rows carry.
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	Turns            int64   `json:"turns"`
	// AgentWallSeconds sums the agents' wall time.
	AgentWallSeconds float64 `json:"agent_wall_seconds"`
}

// totals sums rows.
func totals(rows []RunRow) Totals {
	var t Totals
	val := func(p *int64) int64 {
		if p == nil {
			return 0
		}
		return *p
	}
	for _, r := range rows {
		t.Rows++
		if r.Oracle.Passed {
			t.Passed++
		}
		if r.Valid {
			t.Valid++
		}
		if r.Measured.TurnCapHit != nil && *r.Measured.TurnCapHit {
			t.TurnCapHit++
		}
		t.InputTokens += val(r.Measured.InputTokens)
		t.OutputTokens += val(r.Measured.OutputTokens)
		t.CacheReadTokens += val(r.Measured.CacheReadTokens)
		t.CacheWriteTokens += val(r.Measured.CacheWriteTokens)
		if r.Measured.CostUSD != nil {
			t.CostUSD += *r.Measured.CostUSD
		}
		if r.Measured.Turns != nil {
			t.Turns += int64(*r.Measured.Turns)
		}
		t.AgentWallSeconds += float64(r.Agent.WallMS) / 1000
	}
	return t
}

// writeJSON writes v as indented JSON to path.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// selectExperiments returns the experiments matching only: a package
// import path or a module path; all of them when only is empty.
func selectExperiments(exps []Experiment, only string) ([]Experiment, error) {
	if only == "" {
		return exps, nil
	}
	var out []Experiment
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
