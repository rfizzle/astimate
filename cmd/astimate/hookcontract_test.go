package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// These tests pin the Claude Code Stop hook contract (docs/claude-code-hook.md)
// and call the CLI only through run or the built binary, so they hold however
// the command is implemented behind it.

// hookTestdata returns the path of the Go fixture module name, relative to
// this package's directory.
func hookTestdata(name string) string {
	return filepath.Join("..", "..", "testdata", "go", name)
}

// hookDegradedMetrics are the rules the degraded fixture's tested package
// breaks: a duplicate function, an untested export, a global and one
// function of cognitive complexity 51.
func hookDegradedMetrics() []string {
	return []string{"dup_blocks", "untested_exports", "globals", "changed_func_cognitive_max"}
}

// assertHookBlock checks that out is exactly one JSON object, followed by a
// newline, whose only keys are decision and reason, with decision "block"
// and a reason naming every metric in want and no agent passes or tier.
func assertHookBlock(t *testing.T, out string, want []string) {
	t.Helper()
	if !strings.HasSuffix(out, "}\n") || strings.Count(out, "\n") != 1 {
		t.Errorf("hook stdout = %q, want one JSON object on one line", out)
	}
	dec := json.NewDecoder(strings.NewReader(out))
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil {
		t.Fatalf("decoding hook stdout %q: %v", out, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		t.Errorf("hook stdout holds more than one JSON value: %q", out)
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"decision", "reason"}) {
		t.Fatalf("hook keys = %q, want exactly decision and reason", keys)
	}
	var decision, reason string
	if err := json.Unmarshal(obj["decision"], &decision); err != nil {
		t.Errorf("decision is not a string: %s", obj["decision"])
	}
	if err := json.Unmarshal(obj["reason"], &reason); err != nil {
		t.Errorf("reason is not a string: %s", obj["reason"])
	}
	if decision != "block" {
		t.Errorf("decision = %q, want block", decision)
	}
	for _, m := range want {
		if !strings.Contains(reason, m+": ") {
			t.Errorf("reason does not name %s:\n%s", m, reason)
		}
	}
	// The reason is the findings alone: the agent is told what to fix,
	// never the rebuild estimate's passes or tier, which can contradict it.
	assertNoEstimate(t, reason)
}

// writeHookBaseline writes a baseline of the pristine fixture through the
// CLI and returns its path.
func writeHookBaseline(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "baseline.json")
	var out, errOut bytes.Buffer
	if got := run([]string{"baseline", "write", hookTestdata("fixture"), "--out", path}, &out, &errOut); got != 0 {
		t.Fatalf("baseline write exit code = %d; stderr = %s", got, errOut.String())
	}
	return path
}

func TestHookContract(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	base := writeHookBaseline(t)
	tests := []struct {
		name    string
		fixture string
		block   bool
	}{
		{"degraded blocks", "fixture-degraded", true},
		{"pristine allows", "fixture", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out, errOut bytes.Buffer
			args := []string{"check", "--all", "--baseline", base, "--format", "hook", hookTestdata(tt.fixture)}
			// Claude Code reads a hook's JSON only on exit 0, so a block
			// decision must exit 0 too.
			if got := run(args, &out, &errOut); got != 0 {
				t.Fatalf("run(%q) exit code = %d, want 0\nstdout:\n%s\nstderr:\n%s", args, got, out.String(), errOut.String())
			}
			if !tt.block {
				if out.String() != "{}\n" {
					t.Errorf("hook stdout = %q, want %q", out.String(), "{}\n")
				}
				return
			}
			assertHookBlock(t, out.String(), hookDegradedMetrics())
		})
	}
}

// docSnippet returns the fenced code block between the markers
// "<!-- name:begin -->" and "<!-- name:end -->" in the repository document
// doc, without the fence lines.
func docSnippet(t *testing.T, doc, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", doc))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(data), "<!-- "+name+":begin -->\n")
	block, _, ok2 := strings.Cut(rest, "<!-- "+name+":end -->")
	if !ok || !ok2 {
		t.Fatalf("%s has no %s snippet markers", doc, name)
	}
	lines := strings.Split(strings.TrimSpace(block), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "```") || lines[len(lines)-1] != "```" {
		t.Fatalf("%s snippet %s is not one fenced code block:\n%s", doc, name, block)
	}
	return strings.Join(lines[1:len(lines)-1], "\n") + "\n"
}

// buildAstimate builds the astimate binary into a temporary directory and
// returns that directory, for use on a PATH.
func buildAstimate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	build := exec.CommandContext(t.Context(), "go", "build", "-o", filepath.Join(dir, "astimate"), ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return dir
}

// requireTools skips the test unless every tool is on PATH.
func requireTools(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
}

// stopHookCommand returns the command of the one Stop hook in the
// settings.json snippet of docs/claude-code-hook.md.
func stopHookCommand(t *testing.T) string {
	t.Helper()
	var settings struct {
		Hooks struct {
			Stop []struct {
				Hooks []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
					Timeout int    `json:"timeout"`
				} `json:"hooks"`
			} `json:"Stop"`
		} `json:"hooks"`
	}
	dec := json.NewDecoder(strings.NewReader(docSnippet(t, "claude-code-hook.md", "stop-hook-snippet")))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&settings); err != nil {
		t.Fatalf("decoding the settings.json snippet: %v", err)
	}
	stop := settings.Hooks.Stop
	if len(stop) != 1 || len(stop[0].Hooks) != 1 {
		t.Fatalf("snippet has %d Stop matchers, want one with one hook", len(stop))
	}
	h := stop[0].Hooks[0]
	if h.Type != "command" || h.Command == "" || h.Timeout <= 0 {
		t.Fatalf("snippet hook = %+v, want a command hook with a timeout", h)
	}
	return h.Command
}

// TestStopHookSnippet runs the documented settings.json command as Claude
// Code would, through a shell with the hook input on stdin, in a repository
// whose working tree degrades the fixture since master.
func TestStopHookSnippet(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: builds the binary and runs git")
	}
	requireTools(t, "git", "go", "sh")
	t.Parallel()

	command := stopHookCommand(t)
	env := hookGitEnv(buildAstimate(t))
	repo := newFixtureRepo(t, env)
	gitCmd(t, repo, env, "add", "-A")
	gitCmd(t, repo, env, "commit", "-q", "--no-verify", "-m", "pristine fixture")
	project := filepath.Join(repo, "fixture")
	env = append(env, "CLAUDE_PROJECT_DIR="+project)

	stop := func(t *testing.T, active bool) string {
		t.Helper()
		input, err := json.Marshal(map[string]any{
			"session_id":       "test",
			"transcript_path":  filepath.Join(repo, "transcript.jsonl"),
			"hook_event_name":  "Stop",
			"stop_hook_active": active,
		})
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), "sh", "-c", command)
		cmd.Dir = project
		cmd.Env = env
		cmd.Stdin = bytes.NewReader(input)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("stop hook (stop_hook_active %v): %v\nstdout:\n%s\nstderr:\n%s",
				active, err, stdout.String(), stderr.String())
		}
		return stdout.String()
	}

	if got := stop(t, false); got != "{}\n" {
		t.Errorf("pristine tree: hook stdout = %q, want %q", got, "{}\n")
	}
	degradeFixture(t, repo)
	assertHookBlock(t, stop(t, false), hookDegradedMetrics())
	if got := stop(t, true); got != "{}\n" {
		t.Errorf("stop_hook_active true: hook stdout = %q, want %q so the agent is not blocked twice", got, "{}\n")
	}
}
