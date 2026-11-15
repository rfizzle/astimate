package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// blockLogConfig is capacityConfig with hook.block_log set to logPath.
func blockLogConfig(t *testing.T, logPath string) string {
	t.Helper()
	path := capacityConfig(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("\nhook:\n  block_log: "+logPath+"\n")...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// logLine is the part of a block log line these tests read.
type logLine struct {
	Event   string `json:"event"`
	Package string `json:"package"`
	Metric  string `json:"metric"`
	Block   *struct {
		Kind string  `json:"kind"`
		Head float64 `json:"head"`
	} `json:"block"`
	Pass *struct {
		Value  *float64 `json:"value"`
		Exists bool     `json:"exists"`
	} `json:"pass"`
}

// readBlockLog returns the lines of the log at path; nil when it does not
// exist.
func readBlockLog(t *testing.T, path string) []logLine {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []logLine
	for l := range strings.Lines(string(data)) {
		var ll logLine
		if err := json.Unmarshal([]byte(l), &ll); err != nil {
			t.Fatalf("log line %q: %v", l, err)
		}
		out = append(out, ll)
	}
	return out
}

// hookRun runs check --format hook on dir against base under cfg, with
// stdin as the hook input, and returns its exit code, stdout and stderr.
func hookRun(t *testing.T, dir, base, cfg, stdin string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	in := func() io.Reader { return strings.NewReader(stdin) }
	args := []string{dir, "--all", "--baseline", base, "--config", cfg, "--format", "hook"}
	code := runCheckInput(args, in, &out, &errOut)
	return code, out.String(), errOut.String()
}

// TestHookBlockLog follows one block through the log: a check that blocks
// writes a line per violation, one that passes after the fix writes a
// pass line per blocked metric, and a later pass with nothing open writes
// nothing. At every step the hook's stdout and exit code are those of the
// same check with the log off.
func TestHookBlockLog(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	dir := t.TempDir()
	writeCapacityModule(t, dir, false)
	base := filepath.Join(t.TempDir(), "baseline.json")
	var out, errOut bytes.Buffer
	if got := run([]string{"baseline", "write", dir, "--out", base}, &out, &errOut); got != exitOK {
		t.Fatalf("baseline write exit code = %d; stderr = %s", got, errOut.String())
	}
	on, off := blockLogConfig(t, ".astimate/blocks.jsonl"), capacityConfig(t)
	logPath := filepath.Join(dir, ".astimate", "blocks.jsonl")

	steps := []struct {
		name  string
		p     string // the source of package p; empty leaves the module as it is
		stdin string
		block bool
		want  []string // the new log lines as "event package metric value"
	}{
		{name: "block", p: "-", block: true, want: []string{
			"block p exported_symbols 3", "block p internal_imports 2",
		}},
		{name: "stop_hook_active writes nothing", stdin: `{"stop_hook_active":true}`},
		{name: "pass after the fix", p: "package p\n\nimport \"example.com/capacity/a\"\n\n// One returns a.\nfunc One() int { return a.A() }\n",
			want: []string{"pass p exported_symbols 1", "pass p internal_imports 1"}},
		{name: "pass with nothing open"},
	}
	for _, s := range steps { // sequential: each step reads the log the last one left
		switch s.p {
		case "":
		case "-":
			writeCapacityModule(t, dir, true)
		default:
			if err := os.WriteFile(filepath.Join(dir, "p", "p.go"), []byte(s.p), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		before := len(readBlockLog(t, logPath))
		codeOff, stdoutOff, _ := hookRun(t, dir, base, off, s.stdin)
		codeOn, stdoutOn, stderrOn := hookRun(t, dir, base, on, s.stdin)
		if codeOn != exitOK || codeOn != codeOff || stdoutOn != stdoutOff {
			t.Errorf("%s: log on: exit %d, stdout %q; log off: exit %d, stdout %q; want exit 0 and identical stdout",
				s.name, codeOn, stdoutOn, codeOff, stdoutOff)
		}
		if got := strings.HasPrefix(stdoutOn, `{"decision":"block"`); got != s.block {
			t.Errorf("%s: stdout = %q, want block %v", s.name, stdoutOn, s.block)
		}
		if strings.Contains(stderrOn, "block log") {
			t.Errorf("%s: stderr mentions the block log:\n%s", s.name, stderrOn)
		}
		var got []string
		for _, l := range readBlockLog(t, logPath)[before:] {
			v := "null"
			switch {
			case l.Block != nil:
				v = jsonNumber(l.Block.Head)
			case l.Pass != nil && l.Pass.Value != nil:
				v = jsonNumber(*l.Pass.Value)
			}
			got = append(got, l.Event+" "+l.Package+" "+l.Metric+" "+v)
		}
		if strings.Join(got, "\n") != strings.Join(s.want, "\n") {
			t.Errorf("%s: new log lines =\n%s\nwant\n%s", s.name, strings.Join(got, "\n"), strings.Join(s.want, "\n"))
		}
	}
}

// jsonNumber formats v as JSON does.
func jsonNumber(v float64) string {
	data, _ := json.Marshal(v)
	return string(data)
}

// TestHookBlockLogUnwritable checks that a log that cannot be written
// leaves the decision and the exit code alone and costs one warning on
// stderr.
func TestHookBlockLogUnwritable(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	dir := t.TempDir()
	writeCapacityModule(t, dir, false)
	base := filepath.Join(t.TempDir(), "baseline.json")
	var out, errOut bytes.Buffer
	if got := run([]string{"baseline", "write", dir, "--out", base}, &out, &errOut); got != exitOK {
		t.Fatalf("baseline write exit code = %d; stderr = %s", got, errOut.String())
	}
	writeCapacityModule(t, dir, true)
	// A file where the log's directory should be.
	if err := os.WriteFile(filepath.Join(dir, "notadir"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	codeOff, stdoutOff, _ := hookRun(t, dir, base, capacityConfig(t), "")
	code, stdout, stderr := hookRun(t, dir, base, blockLogConfig(t, "notadir/blocks.jsonl"), "")
	if code != exitOK || code != codeOff || stdout != stdoutOff || !strings.HasPrefix(stdout, `{"decision":"block"`) {
		t.Errorf("exit %d, stdout %q; want exit %d and the log-off stdout %q", code, stdout, codeOff, stdoutOff)
	}
	if n := strings.Count(stderr, "block log not written"); n != 1 {
		t.Errorf("stderr warns %d times about the block log, want once:\n%s", n, stderr)
	}
}

// TestBlockLogSelectsNothing checks that a block log inside the module,
// untracked and not ignored, selects no package and moves no package's
// metrics: it is not a source file of either language.
func TestBlockLogSelectsNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages and runs git")
	}
	t.Parallel()

	dir := t.TempDir()
	writeCapacityModule(t, dir, true)
	gitIn(t, dir, "init", "-q", "-b", "master")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "init")
	for _, name := range []string{".astimate/blocks.jsonl", "blocks.jsonl", "p/blocks.jsonl"} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		line := `{"v":1,"event":"block","module":"example.com/capacity","package":"p","metric":"sloc","block":{"kind":"capacity","base":null,"head":2,"limit":"max 1"}}` + "\n"
		if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errOut bytes.Buffer
	args := []string{"check", dir, "--base", "master", "--format", "json"}
	if got := run(args, &out, &errOut); got != exitOK {
		t.Fatalf("run(%q) exit code = %d; stderr = %s", args, got, errOut.String())
	}
	if reports := decodeReports(t, out.Bytes()); len(reports) != 0 {
		paths := make([]string, 0, len(reports))
		for i := range reports {
			paths = append(paths, reports[i].PackagePath)
		}
		t.Errorf("check selected %q with only block logs changed, want nothing", paths)
	}
}
