package runner

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/agent"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/pin"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/stub"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// fullRow returns a row with every field set to a non-zero, non-null
// value.
func fullRow() RunRow {
	return RunRow{
		Schema: rowSchema, Module: "example.com/mod", Package: "example.com/mod/lib", Dir: "lib",
		Commit: strings.Repeat("a", 40), StubSHA256: strings.Repeat("b", 64), Run: 2, TurnCap: 100, Unit: definition.UnitTree,
		Agent: AgentRun{Name: agent.DefaultName, Template: agent.DefaultTemplate, Command: "claude -p ...",
			Model: agent.DefaultModel, ExitCode: 1, TimedOut: true, WallMS: 1234, StderrTail: "err"},
		Estimate: Estimate{Tier: score.TierOnePass, AgentPasses: 0.4, RebuildTokens: 9000, HumanDays: 1.5, HasTests: true},
		Metrics: metrics.RawMetrics{Files: 1, SLOC: 10, TokensEst: 100, TokensEstWithTests: 150, TestFuncs: 1,
			HasTests: true, UsesCgo: new(false), GeneratedFiles: new(0)},
		Members: []Member{{Package: "example.com/mod/lib", Dir: "lib",
			Estimate: Estimate{Tier: score.TierOnePass, AgentPasses: 0.4, RebuildTokens: 9000, HumanDays: 1.5, HasTests: true},
			Metrics:  metrics.RawMetrics{Files: 1, SLOC: 10, TokensEst: 100, TokensEstWithTests: 150, TestFuncs: 1, HasTests: true}}},
		ConfigVersion: "thresholds-2026-09-27", GoVersion: "go1.27.1",
		Measured: agent.Measured{Usage: agent.Usage{InputTokens: new(int64(1)), OutputTokens: new(int64(2)),
			CacheReadTokens: new(int64(3)), CacheWriteTokens: new(int64(4)), TokenSource: new("modelUsage"),
			CostUSD: new(0.5)}, Turns: new(7),
			ToolCalls: new(9), DurationMS: new(int64(1000)), APIDurationMS: new(int64(800)), SessionID: new("s"),
			ResultSubtype: new("success"), IsError: new(false), TurnCapHit: new(false), Models: []string{"m"},
			Missing: []string{"none"}},
		Oracle: OracleOutcome{Test: []string{"./lib"}, Build: []string{"./..."}, TestsPass: true, BuildPasses: true,
			Passed: true, TestTail: "ok", BuildTail: "ok", WallMS: 99, Completed: true},
		Changes:    Changes{TestFiles: []string{"lib/lib_test.go"}, OutsidePackage: []string{"other/x.go"}},
		Valid:      true,
		StartedAt:  time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 9, 28, 1, 5, 0, 0, time.UTC),
	}
}

// jsonFields returns the JSON names of t's fields, recursing into structs
// this package or the agent package defines (RunRow's own nested types
// plus agent.Measured), as dotted paths.
func jsonFields(t reflect.Type, prefix string) []string {
	agentPkg := reflect.TypeFor[agent.Measured]().PkgPath()
	var out []string
	for f := range t.Fields() {
		if f.Anonymous {
			// An embedded struct's fields encode inline.
			out = append(out, jsonFields(f.Type, prefix)...)
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		path := prefix + name
		out = append(out, path)
		if f.Type.Kind() == reflect.Struct && (f.Type.PkgPath() == t.PkgPath() || f.Type.PkgPath() == agentPkg) {
			out = append(out, jsonFields(f.Type, path+".")...)
		}
	}
	return out
}

// TestRunRowSchema marshals a fully populated row and checks that every
// field of the row type, nested ones included, is present and not null
// under its documented name, and that the row decodes back unchanged.
func TestRunRowSchema(t *testing.T) {
	row := fullRow()
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	fields := jsonFields(reflect.TypeFor[RunRow](), "")
	if len(fields) < 50 {
		t.Fatalf("found only %d fields; the walk is broken", len(fields))
	}
	for _, path := range fields {
		var v any = doc
		for key := range strings.SplitSeq(path, ".") {
			m, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("%s: parent is not an object", path)
			}
			if v, ok = m[key]; !ok {
				t.Fatalf("%s missing from the row", path)
			}
		}
		if v == nil {
			t.Errorf("%s is null in a fully populated row", path)
		}
	}
	// The section 7.1 inputs the fit regresses on travel with each row.
	m := doc["metrics"].(map[string]any)
	for _, k := range []string{"tokens_est", "tokens_est_with_tests", "duplication_pct", "test_funcs", "exported_symbols",
		"fan_in", "untested_exports", "coverage_pct", "globals", "init_funcs", "sloc"} {
		if _, ok := m[k]; !ok {
			t.Errorf("metrics.%s missing", k)
		}
	}
	// A tree row names its unit and carries each member's estimate and
	// metrics.
	if doc["unit"] != definition.UnitTree {
		t.Errorf("unit = %v", doc["unit"])
	}
	mem := doc["members"].([]any)[0].(map[string]any)
	for _, k := range []string{"package", "dir", "estimate", "metrics"} {
		if mem[k] == nil {
			t.Errorf("members[0].%s missing", k)
		}
	}
	if _, ok := mem["estimate"].(map[string]any)["rebuild_tokens"]; !ok {
		t.Error("members[0].estimate.rebuild_tokens missing")
	}
	var back RunRow
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, row) {
		t.Errorf("row does not round-trip:\n got %+v\nwant %+v", back, row)
	}
}

// gitIn runs git in dir.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com",
		"-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// fakeModule creates a one-package module in a git repository and returns
// an experiment for it, pinned at its only commit.
func fakeModule(t *testing.T) definition.Experiment {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	files := map[string]string{
		"go.mod":          "module example.com/fake\n\ngo 1.27\n",
		"lib/lib.go":      "// Package lib adds.\npackage lib\n\n// Add returns a+b.\nfunc Add(a, b int) int {\n\treturn a + b\n}\n",
		"lib/lib_test.go": "package lib\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
		"use/use.go":      "// Package use uses lib.\npackage use\n\nimport \"example.com/fake/lib\"\n\n// Three is 3.\nfunc Three() int { return lib.Add(1, 2) }\n",
	}
	for name, src := range files {
		path := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stubbed, err := stub.Package(t.Context(), filepath.Join(repo, "lib"), nil)
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "init", "--quiet")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "--quiet", "-m", "fake")
	return definition.Experiment{
		Module: "example.com/fake", Repo: "file://" + repo, Commit: gitIn(t, repo, "rev-parse", "HEAD"),
		Package: "example.com/fake/lib", Dir: "lib", Stub: definition.StubSignatures, StubSHA256: stub.TreeHash(stubbed),
		Oracle: definition.Oracle{Test: []string{"./lib"}, Build: []string{"./..."}}, TurnCap: 5, HasTests: true,
		Tier: score.TierOnePass, AgentPasses: 0.1, RebuildTokens: 100, HumanDays: 0.1,
		Metrics: definition.Metrics{RawMetrics: metrics.RawMetrics{Files: 1, SLOC: 3, TokensEst: 20, TokensEstWithTests: 50, FuncCount: 1,
			TestFuncs: 1, HasTests: true, UsesCgo: new(false), GeneratedFiles: new(0)}},
	}
}

// dryRunAgent returns a template running dryrun-agent.sh and logging each
// call to the returned file.
func dryRunAgent(t *testing.T) (tmpl, calls string) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "dryrun-agent.sh"))
	if err != nil {
		t.Fatal(err)
	}
	calls = filepath.Join(t.TempDir(), "calls.log")
	return "sh " + agent.ShellQuote(script) + " {dir} && echo {package} >> " + agent.ShellQuote(calls), calls
}

// newTestRunner returns a Runner over exps writing to out with the agent
// template tmpl.
func newTestRunner(t *testing.T, out, tmpl string) *Runner {
	t.Helper()
	gover, err := pin.GoVersion(t.Context())
	if err != nil {
		t.Skip("go command not found")
	}
	return New(Config{
		Def: &definition.Definition{ConfigVersion: "thresholds-test", Env: append(pin.DefaultEnv(), "GOFLAGS=-mod=mod")},
		Out: out, AgentName: agent.CustomName, Template: tmpl, Model: agent.DefaultModel,
		Timeout: time.Minute, Transcripts: true, GoVersion: gover, Clone: pin.CloneAt,
		Logger: slog.New(slog.DiscardHandler),
	})
}

// callCount returns the number of agent invocations logged in path.
func callCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Count(data, []byte("\n"))
}

// TestResumeSkipsCompletedRuns runs the dry-run agent once per experiment,
// then asks for two runs each: the second invocation must run only the
// missing second runs, and a torn last line (an interrupted write) must be
// dropped and its run redone.
func TestResumeSkipsCompletedRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("clones a local repository and runs go test")
	}
	exps := []definition.Experiment{fakeModule(t)}
	tmpl, calls := dryRunAgent(t)
	out := t.TempDir()

	res, err := newTestRunner(t, out, tmpl).Resume(t.Context(), exps, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Written != 1 || res.Skipped != 0 || len(res.Failures) != 0 || callCount(t, calls) != 1 {
		t.Fatalf("first invocation: %+v, %d agent calls", res, callCount(t, calls))
	}
	row := res.Rows[0]
	if !row.Oracle.Passed || !row.Valid || !row.Oracle.Completed || len(row.Measured.Missing) != 0 {
		t.Fatalf("dry-run row not fully populated: %+v", row)
	}

	res, err = newTestRunner(t, out, tmpl).Resume(t.Context(), exps, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Written != 1 || res.Skipped != 1 || callCount(t, calls) != 2 {
		t.Fatalf("second invocation: %+v, %d agent calls", res, callCount(t, calls))
	}
	runs := func(rows []RunRow) []int {
		var r []int
		for _, row := range rows {
			r = append(r, row.Run)
		}
		return r
	}
	if got := runs(res.Rows); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("runs %v, want [1 2]", got)
	}

	// Nothing left: a third invocation starts no agent.
	res, err = newTestRunner(t, out, tmpl).Resume(t.Context(), exps, 2, 1)
	if err != nil || res.Written != 0 || res.Skipped != 2 || callCount(t, calls) != 2 {
		t.Fatalf("third invocation: %+v, %v, %d agent calls", res, err, callCount(t, calls))
	}

	// A torn write of run 3 is dropped, and run 3 runs again.
	f, err := os.OpenFile(filepath.Join(out, RunsFile), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"schema":1,"package":"example.com/fake/lib","run":3,"oracle":{"compl`); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	res, err = newTestRunner(t, out, tmpl).Resume(t.Context(), exps, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := runs(res.Rows); res.Written != 1 || !slices.Equal(got, []int{1, 2, 3}) || callCount(t, calls) != 3 {
		t.Fatalf("after a torn write: runs %v, %+v", got, res)
	}

	// Another agent's rows are refused rather than mixed in.
	other := newTestRunner(t, out, tmpl)
	other.Model = "another-model"
	if _, err := other.Resume(t.Context(), exps, 3, 1); err == nil {
		t.Error("rows of another model were resumed")
	}
}

// TestRunRecordsBrokenRules checks that editing a test file or another
// package marks the row invalid.
func TestRunRecordsBrokenRules(t *testing.T) {
	if testing.Short() {
		t.Skip("clones a local repository and runs go test")
	}
	exps := []definition.Experiment{fakeModule(t)}
	script, err := filepath.Abs(filepath.Join("..", "..", "dryrun-agent.sh"))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := "sh " + agent.ShellQuote(script) + " {dir} && echo '// x' >> lib/lib_test.go && echo '// y' >> use/use.go"
	res, err := newTestRunner(t, t.TempDir(), tmpl).Resume(t.Context(), exps, 1, 1)
	if err != nil || res.Written != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	c := res.Rows[0].Changes
	if res.Rows[0].Valid || !slices.Equal(c.TestFiles, []string{"lib/lib_test.go"}) ||
		!slices.Equal(c.OutsidePackage, []string{"use/use.go"}) {
		t.Errorf("changes %+v valid %v", c, res.Rows[0].Valid)
	}
}

// TestTailBuffer checks that the buffer keeps the last bytes written.
func TestTailBuffer(t *testing.T) {
	var buf bytes.Buffer
	tb := &tailBuffer{max: 5, buf: &buf}
	for _, s := range []string{"abc", "defg", "0123456789"} {
		n, err := tb.Write([]byte(s))
		if err != nil || n != len(s) {
			t.Fatal(n, err)
		}
	}
	if buf.String() != "56789" {
		t.Errorf("tail %q", buf.String())
	}
	// io.Writer is also satisfied through the interface.
	var _ io.Writer = tb
}

// TestAgentThatNeverStartsIsRetried checks that an agent exiting with an
// error and no result writes no row, so a later invocation retries the
// run, and that the runner stops after three such runs in a row.
func TestAgentThatNeverStartsIsRetried(t *testing.T) {
	if testing.Short() {
		t.Skip("clones a local repository and runs go test")
	}
	exps := []definition.Experiment{fakeModule(t)}
	calls := filepath.Join(t.TempDir(), "calls.log")
	tmpl := "echo {package} >> " + agent.ShellQuote(calls) + "; echo 'error: unknown option' >&2; exit 1"
	res, err := newTestRunner(t, t.TempDir(), tmpl).Resume(t.Context(), exps, 5, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Written != 0 || len(res.Rows) != 0 || len(res.Failures) != 3 || callCount(t, calls) != 3 {
		t.Fatalf("%+v, %d agent calls; want 3 failures, no rows, then a stop", res, callCount(t, calls))
	}
	if !strings.Contains(res.Failures[0], "unknown option") {
		t.Errorf("failure does not carry the agent's error: %s", res.Failures[0])
	}
}

// TestReadFile checks that ReadFile returns the file's bytes, nil, nil for
// a missing file, and an error for another failure.
func TestReadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := ReadFile(path)
	if err != nil || string(data) != "hi" {
		t.Fatalf("ReadFile() = %q, %v", data, err)
	}
	data, err = ReadFile(filepath.Join(t.TempDir(), "missing"))
	if err != nil || data != nil {
		t.Fatalf("ReadFile() of a missing file = %q, %v, want nil, nil", data, err)
	}
}

// TestParseRuns checks that ParseRuns decodes complete lines and ignores a
// final line with no trailing newline.
func TestParseRuns(t *testing.T) {
	data := []byte(`{"schema":1,"package":"a","run":1}` + "\n" + `{"schema":1,"package":"b","run":1` /* torn */)
	rows, err := ParseRuns("runs.jsonl", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Package != "a" {
		t.Fatalf("ParseRuns() = %+v", rows)
	}
}

// TestCompleted checks that Completed collects the resume keys of rows
// whose oracle finished, and refuses rows from another agent.
func TestCompleted(t *testing.T) {
	exps := []definition.Experiment{{Package: "a", StubSHA256: "h1"}}
	rows := []RunRow{
		{Package: "a", Run: 1, StubSHA256: "h1", Agent: AgentRun{Name: "n", Template: "t", Model: "m"}, Oracle: OracleOutcome{Completed: true}},
		{Package: "a", Run: 2, StubSHA256: "h1", Agent: AgentRun{Name: "n", Template: "t", Model: "m"}},
	}
	done, err := Completed(rows, definition.UnitPackage, "n", "t", "m", exps)
	if err != nil {
		t.Fatal(err)
	}
	if !done[RunKey{"a", 1}] || done[RunKey{"a", 2}] {
		t.Fatalf("Completed() = %v", done)
	}
	if _, err := Completed(rows, definition.UnitPackage, "other", "t", "m", exps); err == nil {
		t.Fatal("Completed accepted rows from another agent")
	}
	// Rows written before rows had a unit are package rows: a tree
	// definition refuses them, and tree rows refuse a package definition.
	if _, err := Completed(rows, definition.UnitTree, "n", "t", "m", exps); err == nil || !strings.Contains(err.Error(), "unit") {
		t.Fatalf("Completed mixed package rows into a tree run: %v", err)
	}
	if RowUnit(&rows[0]) != definition.UnitPackage {
		t.Fatalf("a row without a unit reads as %q", RowUnit(&rows[0]))
	}
	rows[0].Unit = definition.UnitTree
	if RowUnit(&rows[0]) != definition.UnitTree {
		t.Fatal("RowUnit ignores the row's unit")
	}
	if _, err := Completed(rows, definition.UnitPackage, "n", "t", "m", exps); err == nil {
		t.Fatal("Completed mixed tree rows into a package run")
	}
}

// TestPending checks that Pending lists the missing runs in run-major
// order.
func TestPending(t *testing.T) {
	exps := []definition.Experiment{{Package: "a"}, {Package: "b"}}
	done := map[RunKey]bool{{"a", 1}: true}
	jobs := Pending(exps, 2, done)
	var got []string
	for _, j := range jobs {
		got = append(got, j.Exp.Package+strconv.Itoa(j.Run))
	}
	if want := []string{"b1", "a2", "b2"}; !slices.Equal(got, want) {
		t.Fatalf("Pending() = %v, want %v", got, want)
	}
}

// TestSumTotals checks that SumTotals sums the rows' measurements.
func TestSumTotals(t *testing.T) {
	rows := []RunRow{fullRow(), fullRow()}
	tot := SumTotals(rows)
	if tot.Rows != 2 || tot.Passed != 2 || tot.Valid != 2 || tot.TurnCapHit != 0 {
		t.Fatalf("SumTotals() = %+v", tot)
	}
	if tot.InputTokens != 2 || tot.OutputTokens != 4 || tot.Turns != 14 || tot.CostUSD != 1 {
		t.Fatalf("SumTotals() sums = %+v", tot)
	}
}

// TestWriteJSON checks that WriteJSON writes indented JSON ending in a
// newline.
func TestWriteJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	if err := WriteJSON(path, map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\"a\": 1") || !strings.HasSuffix(string(data), "}\n") {
		t.Fatalf("WriteJSON wrote %q", data)
	}
}

// TestUsageError checks that UsageError prefixes its cause's message and
// unwraps to it.
func TestUsageError(t *testing.T) {
	cause := errors.New("boom")
	e := &UsageError{err: cause}
	if got, want := e.Error(), "usage: boom"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if e.Unwrap() != cause {
		t.Fatalf("Unwrap() = %v, want %v", e.Unwrap(), cause)
	}
}
