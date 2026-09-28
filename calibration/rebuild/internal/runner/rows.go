package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/agent"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// rowSchema is the version of the RunRow layout; it changes when a field
// is renamed or removed.
const rowSchema = 1

// RunsFile and RunInfoFile are the runner's outputs in its directory.
const (
	RunsFile    = "runs.jsonl"
	RunInfoFile = "run.json"
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
	Measured agent.Measured `json:"measured"`
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

// RunKey is the resume key of a run.
type RunKey struct {
	pkg string
	run int
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

// SumTotals sums rows.
func SumTotals(rows []RunRow) Totals {
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

// WriteJSON writes v as indented JSON to path.
func WriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
