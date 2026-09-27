package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// serveChildEnv marks a re-executed test binary that runs `astimate serve`.
const serveChildEnv = "ASTIMATE_TEST_SERVE"

// TestServeChild is the body of the child process the serve tests start: it
// runs the real main with `serve` on the process's own stdin and stdout. It
// does nothing in a normal test run.
func TestServeChild(t *testing.T) {
	if os.Getenv(serveChildEnv) != "1" {
		t.Skip("helper process for the serve tests")
	}
	os.Args = []string{"astimate", "serve"}
	main()
}

// serveReadyLog is the log line mcpserver writes once its session is up.
const serveReadyLog = "mcp session started"

// logSink collects a child's stderr and closes ready once serveReadyLog has
// been written. exec copies stderr from its own goroutine, so access is
// locked.
type logSink struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	ready chan struct{}
	seen  bool
}

func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.buf.Write(p)
	if !s.seen && strings.Contains(s.buf.String(), serveReadyLog) {
		s.seen = true
		close(s.ready)
	}
	return n, err
}

func (s *logSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// startServe starts `astimate serve` in a child process and returns it with
// pipes to its stdin and stdout and the sink collecting its stderr. It
// returns once the server has logged that its session is up, so a caller
// timing shutdown does not also time process start, which on a fresh test
// binary can take most of a second.
func startServe(t *testing.T) (*exec.Cmd, io.WriteCloser, io.ReadCloser, *logSink) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestServeChild$")
	// Under -race the runtime sleeps a second before exiting by default,
	// which would hide how fast serve itself exits.
	cmd.Env = append(os.Environ(), serveChildEnv+"=1", "GORACE=atexit_sleep_ms=0")
	cmd.Dir = t.TempDir() // no astimate.yaml: the embedded default config
	stderr := &logSink{ready: make(chan struct{})}
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting serve: %v", err)
	}
	select {
	case <-stderr.ready:
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("serve did not start; stderr:\n%s", stderr)
	}
	return cmd, stdin, stdout, stderr
}

// waitExit waits up to limit for cmd to exit and returns its exit code.
func waitExit(t *testing.T, cmd *exec.Cmd, limit time.Duration) int {
	t.Helper()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		switch {
		case err == nil:
			return exitOK
		case errors.As(err, &exitErr):
			return exitErr.ExitCode()
		default:
			t.Fatalf("waiting for serve: %v", err)
		}
	case <-time.After(limit):
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("serve did not exit within %v of stdin closing", limit)
	}
	return -1
}

// frame is the part of a JSON-RPC message the serve tests inspect.
type frame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
}

func TestServeStdoutCarriesOnlyProtocolFrames(t *testing.T) {
	t.Parallel()

	cmd, stdin, stdout, stderr := startServe(t)

	// A legacy initialize handshake, then tools/list.
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18",` +
			`"capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
	}
	for _, r := range requests {
		if _, err := io.WriteString(stdin, r+"\n"); err != nil {
			t.Fatalf("writing request: %v", err)
		}
	}

	// Read every stdout line until the tools/list response arrives, then
	// close stdin and drain the rest. The watchdog ends a server that never
	// answers, which closes stdout and ends the scan.
	watchdog := time.AfterFunc(10*time.Second, func() { _ = cmd.Process.Kill() })
	defer watchdog.Stop()
	var lines []string
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	closed := false
	for sc.Scan() {
		line := sc.Text()
		lines = append(lines, line)
		var f frame
		if json.Unmarshal([]byte(line), &f) == nil && string(f.ID) == "2" && !closed {
			if err := stdin.Close(); err != nil {
				t.Fatalf("closing stdin: %v", err)
			}
			closed = true
		}
	}
	if !closed {
		_ = stdin.Close()
	}
	if code := waitExit(t, cmd, 5*time.Second); code != exitOK {
		t.Errorf("serve exit code = %d, want %d; stderr:\n%s", code, exitOK, stderr)
	}

	byID := map[string]frame{}
	for _, line := range lines {
		var f frame
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			t.Errorf("stdout line is not a JSON object: %q: %v", line, err)
			continue
		}
		if f.JSONRPC != "2.0" {
			t.Errorf("stdout line is not a JSON-RPC 2.0 frame: %q", line)
		}
		byID[string(f.ID)] = f
	}

	var init struct {
		ServerInfo struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(byID["1"].Result, &init); err != nil || init.ServerInfo.Name != "astimate" {
		t.Errorf("initialize result = %s (err %v), want serverInfo.name astimate", byID["1"].Result, err)
	}
	var list struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(byID["2"].Result, &list); err != nil || list.Tools == nil || len(list.Tools) != 0 {
		t.Errorf("tools/list result = %s (err %v), want an empty tools array", byID["2"].Result, err)
	}
	if !strings.Contains(stderr.String(), "mcp server starting") {
		t.Errorf("stderr = %q, want the startup log line", stderr)
	}
}

func TestServeExitsOnStdinClose(t *testing.T) {
	t.Parallel()

	cmd, stdin, stdout, stderr := startServe(t)
	var out []byte
	read := make(chan error, 1)
	go func() {
		var err error
		out, err = io.ReadAll(stdout) // EOF when the child exits
		read <- err
	}()

	closedAt := time.Now()
	if err := stdin.Close(); err != nil {
		t.Fatalf("closing stdin: %v", err)
	}
	select {
	case err := <-read:
		if err != nil {
			t.Fatalf("reading stdout: %v", err)
		}
	case <-time.After(time.Second):
		_ = cmd.Process.Kill()
		<-read
		_ = cmd.Wait()
		t.Fatalf("serve did not exit within one second of stdin closing; stderr:\n%s", stderr)
	}
	if code := waitExit(t, cmd, time.Second); code != exitOK {
		t.Errorf("serve exit code = %d, want %d; stderr:\n%s", code, exitOK, stderr)
	}
	if elapsed := time.Since(closedAt); elapsed > time.Second {
		t.Errorf("serve exited %v after stdin closed, want within one second", elapsed)
	}
	if len(out) != 0 {
		t.Errorf("stdout = %q, want empty with no requests", out)
	}
}

func TestRunServeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string
	}{
		{name: "positional argument", args: []string{"serve", "extra"}, wantCode: exitUsage, wantErr: "takes no arguments"},
		{name: "unknown flag", args: []string{"serve", "--nope"}, wantCode: exitUsage, wantErr: "usage: astimate serve"},
		{
			name:     "missing config file",
			args:     []string{"serve", "--config", filepath.Join(t.TempDir(), "missing.yaml")},
			wantCode: exitAnalysis,
			wantErr:  "resolving config",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			got := run(tt.args, &stdout, &stderr)

			if got != tt.wantCode {
				t.Errorf("run(%q) exit code = %d, want %d", tt.args, got, tt.wantCode)
			}
			if !strings.Contains(stderr.String(), tt.wantErr) {
				t.Errorf("run(%q) stderr = %q, want %q", tt.args, stderr.String(), tt.wantErr)
			}
			if stdout.Len() != 0 {
				t.Errorf("run(%q) stdout = %q, want empty", tt.args, stdout.String())
			}
		})
	}
}
