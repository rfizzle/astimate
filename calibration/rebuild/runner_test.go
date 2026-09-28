package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// fullRow returns a row with every field set to a non-zero, non-null
// value.
func fullRow() RunRow {
	return RunRow{
		Schema: rowSchema, Module: "example.com/mod", Package: "example.com/mod/lib", Dir: "lib",
		Commit: strings.Repeat("a", 40), StubSHA256: strings.Repeat("b", 64), Run: 2, TurnCap: 100,
		Agent: AgentRun{Name: defaultAgentName, Template: defaultAgentTemplate, Command: "claude -p ...",
			Model: defaultModel, ExitCode: 1, TimedOut: true, WallMS: 1234, StderrTail: "err"},
		Estimate: Estimate{Tier: score.TierOnePass, AgentPasses: 0.4, RebuildTokens: 9000, HumanDays: 1.5, HasTests: true},
		Metrics: metrics.RawMetrics{Files: 1, SLOC: 10, TokensEst: 100, TokensEstWithTests: 150, TestFuncs: 1,
			HasTests: true, UsesCgo: new(false), GeneratedFiles: new(0)},
		ConfigVersion: "thresholds-2026-09-27", GoVersion: "go1.27.1",
		Measured: Measured{InputTokens: new(int64(1)), OutputTokens: new(int64(2)), CacheReadTokens: new(int64(3)),
			CacheWriteTokens: new(int64(4)), TokenSource: new("modelUsage"), CostUSD: new(0.5), Turns: new(7),
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

// jsonFields returns the JSON names of t's fields, recursing into the
// structs this package defines, as dotted paths.
func jsonFields(t reflect.Type, prefix string) []string {
	var out []string
	for f := range t.Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		path := prefix + name
		out = append(out, path)
		if f.Type.Kind() == reflect.Struct && f.Type.PkgPath() == t.PkgPath() {
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
	var back RunRow
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, row) {
		t.Errorf("row does not round-trip:\n got %+v\nwant %+v", back, row)
	}
}

func TestParseAgentOutput(t *testing.T) {
	stream := `{"type":"system","subtype":"init","session_id":"abc","tools":["Read"]}
{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"hi"},{"type":"tool_use","id":"t1","name":"Read"}]}}
{"type":"assistant","message":{"id":"m1","content":[{"type":"tool_use","id":"t2","name":"Edit"}]}}
{"type":"user","message":{"content":"a string, not blocks"}}
{"type":"assistant","message":{"id":"m2","content":[{"type":"tool_use","id":"t2","name":"Edit"}]}}
not json at all
{"type":"result","subtype":"success","is_error":false,"duration_ms":5000,"duration_api_ms":4000,"num_turns":12,` +
		`"session_id":"abc","total_cost_usd":0.25,"future_field":{"x":1},` +
		`"usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":30,"cache_read_input_tokens":40},` +
		`"modelUsage":{"model-b":{"inputTokens":1,"outputTokens":2,"cacheReadInputTokens":3,"cacheCreationInputTokens":4},` +
		`"model-a":{"inputTokens":10,"outputTokens":20,"cacheReadInputTokens":30,"cacheCreationInputTokens":40}}}
`
	tests := []struct {
		name    string
		out     string
		turnCap int
		check   func(t *testing.T, m Measured)
	}{
		{"stream-json", stream, 100, func(t *testing.T, m Measured) {
			if *m.InputTokens != 11 || *m.OutputTokens != 22 || *m.CacheReadTokens != 33 || *m.CacheWriteTokens != 44 {
				t.Errorf("tokens %d %d %d %d, want the modelUsage sums", *m.InputTokens, *m.OutputTokens,
					*m.CacheReadTokens, *m.CacheWriteTokens)
			}
			if *m.TokenSource != "modelUsage" || *m.ToolCalls != 2 || *m.Turns != 12 || *m.CostUSD != 0.25 ||
				*m.SessionID != "abc" || *m.DurationMS != 5000 || *m.APIDurationMS != 4000 || *m.TurnCapHit {
				t.Errorf("unexpected %+v", m)
			}
			if !slices.Equal(m.Models, []string{"model-a", "model-b"}) || len(m.Missing) != 0 {
				t.Errorf("models %v missing %v", m.Models, m.Missing)
			}
		}},
		{"json result pretty printed", `{
  "type": "result", "subtype": "error_max_turns", "is_error": true, "num_turns": 101,
  "usage": {"input_tokens": 5, "output_tokens": 6}
}`, 100, func(t *testing.T, m Measured) {
			if *m.TokenSource != "usage" || *m.InputTokens != 5 || !*m.TurnCapHit || !*m.IsError {
				t.Errorf("unexpected %+v", m)
			}
			want := []string{"cache_read_tokens", "cache_write_tokens", "cost_usd", "tool_calls", "duration_ms",
				"api_duration_ms", "session_id", "models"}
			if !slices.Equal(m.Missing, want) {
				t.Errorf("missing %v, want %v", m.Missing, want)
			}
		}},
		{"turns reach the cap", `{"type":"result","num_turns":100}`, 100, func(t *testing.T, m Measured) {
			if !*m.TurnCapHit {
				t.Error("turn cap not detected")
			}
		}},
		{"no output", "", 100, func(t *testing.T, m Measured) {
			if m.InputTokens != nil || m.TurnCapHit != nil || m.ToolCalls != nil || len(m.Missing) != 15 {
				t.Errorf("want everything null, got %+v", m)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, ParseAgentOutput([]byte(tt.out), tt.turnCap))
		})
	}
}

func TestAgentTemplate(t *testing.T) {
	if err := checkTemplate(defaultAgentTemplate); err != nil {
		t.Fatal(err)
	}
	if err := checkTemplate(`x {nope}`); err == nil {
		t.Error("unknown placeholder accepted")
	}
	if err := checkTemplate(`echo "${HOME}" {dir} {X}`); err != nil {
		t.Errorf("shell expansion or an upper-case brace taken for a placeholder: %v", err)
	}
	got := renderTemplate(`run {dir} {prompt_file} "${dir}" {turn_cap}`, map[string]string{
		"dir": "a b", "prompt_file": "/tmp/it's", "turn_cap": "100",
	})
	want := `run 'a b' '/tmp/it'\''s' "${dir}" 100`
	if got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
	for tmpl, want := range map[string]bool{
		defaultAgentTemplate:                  true,
		"/usr/local/bin/claude -p x":          true,
		`sh -c "claude -p x"`:                 true,
		"sh /repo/dryrun-agent.sh {dir}":      false,
		"echo claude-code {dir}":              false,
		"sh ./calibration/rebuild/claude.txt": false,
	} {
		if got := callsClaude(tmpl); got != want {
			t.Errorf("callsClaude(%q) = %v, want %v", tmpl, got, want)
		}
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
func fakeModule(t *testing.T) Experiment {
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
	stub, err := StubPackage(filepath.Join(repo, "lib"))
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "init", "--quiet")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "--quiet", "-m", "fake")
	return Experiment{
		Module: "example.com/fake", Repo: "file://" + repo, Commit: gitIn(t, repo, "rev-parse", "HEAD"),
		Package: "example.com/fake/lib", Dir: "lib", Stub: StubSignatures, StubSHA256: TreeHash(stub),
		Oracle: Oracle{Test: []string{"./lib"}, Build: []string{"./..."}}, TurnCap: 5, HasTests: true,
		Tier: score.TierOnePass, AgentPasses: 0.1, RebuildTokens: 100, HumanDays: 0.1,
		Metrics: Metrics{metrics.RawMetrics{Files: 1, SLOC: 3, TokensEst: 20, TokensEstWithTests: 50, FuncCount: 1,
			TestFuncs: 1, HasTests: true, UsesCgo: new(false), GeneratedFiles: new(0)}},
	}
}

// dryRunAgent returns a template running dryrun-agent.sh and logging each
// call to the returned file.
func dryRunAgent(t *testing.T) (tmpl, calls string) {
	t.Helper()
	script, err := filepath.Abs("dryrun-agent.sh")
	if err != nil {
		t.Fatal(err)
	}
	calls = filepath.Join(t.TempDir(), "calls.log")
	return "sh " + shellQuote(script) + " {dir} && echo {package} >> " + shellQuote(calls), calls
}

// newTestRunner returns a runner over exps writing to out with the agent
// template tmpl.
func newTestRunner(t *testing.T, out, tmpl string) *runner {
	t.Helper()
	gover, err := goVersion(t.Context())
	if err != nil {
		t.Skip("go command not found")
	}
	return &runner{
		def: &Definition{ConfigVersion: "thresholds-test", Env: append(defaultEnv(), "GOFLAGS=-mod=mod")},
		out: out, agentName: customAgentName, template: tmpl, model: defaultModel,
		timeout: time.Minute, transcripts: true, goVersion: gover, clone: CloneAt,
		logger: slog.New(slog.DiscardHandler),
	}
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
	exps := []Experiment{fakeModule(t)}
	tmpl, calls := dryRunAgent(t)
	out := t.TempDir()

	res, err := newTestRunner(t, out, tmpl).resume(t.Context(), exps, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.written != 1 || res.skipped != 0 || len(res.failures) != 0 || callCount(t, calls) != 1 {
		t.Fatalf("first invocation: %+v, %d agent calls", res, callCount(t, calls))
	}
	row := res.rows[0]
	if !row.Oracle.Passed || !row.Valid || !row.Oracle.Completed || len(row.Measured.Missing) != 0 {
		t.Fatalf("dry-run row not fully populated: %+v", row)
	}

	res, err = newTestRunner(t, out, tmpl).resume(t.Context(), exps, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.written != 1 || res.skipped != 1 || callCount(t, calls) != 2 {
		t.Fatalf("second invocation: %+v, %d agent calls", res, callCount(t, calls))
	}
	runs := func(rows []RunRow) []int {
		var r []int
		for _, row := range rows {
			r = append(r, row.Run)
		}
		return r
	}
	if got := runs(res.rows); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("runs %v, want [1 2]", got)
	}

	// Nothing left: a third invocation starts no agent.
	res, err = newTestRunner(t, out, tmpl).resume(t.Context(), exps, 2, 1)
	if err != nil || res.written != 0 || res.skipped != 2 || callCount(t, calls) != 2 {
		t.Fatalf("third invocation: %+v, %v, %d agent calls", res, err, callCount(t, calls))
	}

	// A torn write of run 3 is dropped, and run 3 runs again.
	f, err := os.OpenFile(filepath.Join(out, runsFile), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"schema":1,"package":"example.com/fake/lib","run":3,"oracle":{"compl`); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	res, err = newTestRunner(t, out, tmpl).resume(t.Context(), exps, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := runs(res.rows); res.written != 1 || !slices.Equal(got, []int{1, 2, 3}) || callCount(t, calls) != 3 {
		t.Fatalf("after a torn write: runs %v, %+v", got, res)
	}

	// Another agent's rows are refused rather than mixed in.
	other := newTestRunner(t, out, tmpl)
	other.model = "another-model"
	if _, err := other.resume(t.Context(), exps, 3, 1); err == nil {
		t.Error("rows of another model were resumed")
	}
}

// TestRunRecordsBrokenRules checks that editing a test file or another
// package marks the row invalid.
func TestRunRecordsBrokenRules(t *testing.T) {
	if testing.Short() {
		t.Skip("clones a local repository and runs go test")
	}
	exps := []Experiment{fakeModule(t)}
	script, err := filepath.Abs("dryrun-agent.sh")
	if err != nil {
		t.Fatal(err)
	}
	tmpl := "sh " + shellQuote(script) + " {dir} && echo '// x' >> lib/lib_test.go && echo '// y' >> use/use.go"
	res, err := newTestRunner(t, t.TempDir(), tmpl).resume(t.Context(), exps, 1, 1)
	if err != nil || res.written != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	c := res.rows[0].Changes
	if res.rows[0].Valid || !slices.Equal(c.TestFiles, []string{"lib/lib_test.go"}) ||
		!slices.Equal(c.OutsidePackage, []string{"use/use.go"}) {
		t.Errorf("changes %+v valid %v", c, res.rows[0].Valid)
	}
}

// TestRunNeedsLive checks that the run command never starts Claude Code
// without --live: a claude on PATH that records its calls is never
// called, and nothing is written.
func TestRunNeedsLive(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(bin, "called")
	fake := "#!/bin/sh\ntouch " + shellQuote(marker) + "\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	tests := []struct {
		name string
		args []string
		code int
	}{
		{"default agent", nil, exitUsage},
		{"one experiment", []string{"--only", "google.golang.org/grpc/resolver/manual"}, exitUsage},
		{"template calling claude", []string{"--agent", "claude -p x"}, exitUsage},
		{"plan", []string{"--plan"}, exitOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			var stdout, stderr bytes.Buffer
			args := append([]string{"run", "--definition", "rebuild.yaml", "--out", out}, tt.args...)
			if code := run(context.Background(), args, &stdout, &stderr); code != tt.code {
				t.Fatalf("exit %d, want %d; stderr:\n%s", code, tt.code, stderr.String())
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("claude was started")
			}
			if _, err := os.Stat(out); err == nil {
				t.Fatal("the output directory was created")
			}
			if tt.code == exitUsage && !strings.Contains(stderr.String(), "--live") {
				t.Errorf("refusal does not say how to run live:\n%s", stderr.String())
			}
			if tt.code == exitOK && !strings.Contains(stdout.String(), "102 runs pending") {
				t.Errorf("plan output:\n%s", stdout.String())
			}
		})
	}
}

// TestTailBuffer checks that the buffer keeps the last bytes written.
func TestTailBuffer(t *testing.T) {
	var buf bytes.Buffer
	tb := &tailBuffer{max: 5, buf: &buf}
	for _, s := range []string{"abc", "defg", "0123456789"} {
		if n, err := io.WriteString(tb, s); err != nil || n != len(s) {
			t.Fatal(n, err)
		}
	}
	if buf.String() != "56789" {
		t.Errorf("tail %q", buf.String())
	}
}

// TestAgentThatNeverStartsIsRetried checks that an agent exiting with an
// error and no result writes no row, so a later invocation retries the
// run, and that the runner stops after three such runs in a row.
func TestAgentThatNeverStartsIsRetried(t *testing.T) {
	if testing.Short() {
		t.Skip("clones a local repository and runs go test")
	}
	exps := []Experiment{fakeModule(t)}
	calls := filepath.Join(t.TempDir(), "calls.log")
	tmpl := "echo {package} >> " + shellQuote(calls) + "; echo 'error: unknown option' >&2; exit 1"
	res, err := newTestRunner(t, t.TempDir(), tmpl).resume(t.Context(), exps, 5, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.written != 0 || len(res.rows) != 0 || len(res.failures) != 3 || callCount(t, calls) != 3 {
		t.Fatalf("%+v, %d agent calls; want 3 failures, no rows, then a stop", res, callCount(t, calls))
	}
	if !strings.Contains(res.failures[0], "unknown option") {
		t.Errorf("failure does not carry the agent's error: %s", res.failures[0])
	}
}
