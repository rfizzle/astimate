package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// modernVersion is the protocol revision whose clients send server/discover
// and a per-request _meta instead of the initialize handshake.
const modernVersion = "2026-07-28"

// legacyVersion is a protocol revision negotiated with initialize.
const legacyVersion = "2025-06-18"

// JSON-RPC error codes the protocol tests expect.
const (
	codeInvalidParams              = -32602
	codeUnsupportedProtocolVersion = -32022
)

// responseTimeout bounds the wait for one response. check_package loads and
// extracts a module, which under -race on a loaded machine takes seconds.
const responseTimeout = 60 * time.Second

// requestMeta returns the per-request _meta a 2026-07-28 client sends,
// naming version as its protocol version.
func requestMeta(version string) string {
	return `{"io.modelcontextprotocol/protocolVersion":` + strconv.Quote(version) +
		`,"io.modelcontextprotocol/clientInfo":{"name":"test","version":"0"}` +
		`,"io.modelcontextprotocol/clientCapabilities":{}}`
}

// rpcResponse is a JSON-RPC response frame.
type rpcResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// stdioSession drives `astimate serve` in a child process with hand-written
// JSON-RPC frames, one per stdin line, and records every stdout line.
type stdioSession struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *logSink
	lines  chan string // stdout lines; closed at EOF
	seen   []string    // every stdout line read so far
	ids    []string    // ids of the requests sent, in order
}

// startStdioSession starts serve with --allow-any-path in dir.
func startStdioSession(t *testing.T, dir string) *stdioSession {
	t.Helper()
	cmd, stdin, stdout, stderr := startServeIn(t, dir, serveChildAllowAnyPathEnv+"=1")
	s := &stdioSession{t: t, cmd: cmd, stdin: stdin, stderr: stderr, lines: make(chan string)}
	go func() {
		defer close(s.lines)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
	}()
	return s
}

// send writes one frame. A frame with a non-empty id is a request whose
// response the framing check expects exactly once.
func (s *stdioSession) send(id, frame string) {
	s.t.Helper()
	if _, err := io.WriteString(s.stdin, frame+"\n"); err != nil {
		s.t.Fatalf("writing %s: %v", frame, err)
	}
	if id != "" {
		s.ids = append(s.ids, id)
	}
}

// call sends a request with id, method and params and returns its response.
func (s *stdioSession) call(id int, method, params string) rpcResponse {
	s.t.Helper()
	key := strconv.Itoa(id)
	s.send(key, `{"jsonrpc":"2.0","id":`+key+`,"method":"`+method+`","params":`+params+`}`)
	deadline := time.After(responseTimeout)
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				s.t.Fatalf("stdout closed before the response to %s; stderr:\n%s", method, s.stderr)
			}
			s.seen = append(s.seen, line)
			var r rpcResponse
			if json.Unmarshal([]byte(line), &r) == nil && string(r.ID) == key {
				return r
			}
		case <-deadline:
			s.t.Fatalf("no response to %s within %v; stderr:\n%s", method, responseTimeout, s.stderr)
		}
	}
}

// close closes stdin, reads stdout to EOF, checks that serve exits 0, and
// checks the framing of everything it wrote.
func (s *stdioSession) close() {
	s.t.Helper()
	if err := s.stdin.Close(); err != nil {
		s.t.Fatalf("closing stdin: %v", err)
	}
	deadline := time.After(responseTimeout)
	for done := false; !done; {
		select {
		case line, ok := <-s.lines:
			if ok {
				s.seen = append(s.seen, line)
			}
			done = !ok
		case <-deadline:
			s.t.Fatalf("stdout not closed within %v of stdin closing; stderr:\n%s", responseTimeout, s.stderr)
		}
	}
	if code := waitExit(s.t, s.cmd, 5*time.Second); code != exitOK {
		s.t.Errorf("serve exit code = %d, want %d; stderr:\n%s", code, exitOK, s.stderr)
	}
	checkFraming(s.t, s.seen, s.ids)
}

// checkFraming checks that each stdout line is exactly one JSON-RPC 2.0
// object, and that the lines are exactly one response to each request id,
// so nothing else reached stdout: no blank line, no log, no stray frame.
func checkFraming(t *testing.T, lines, ids []string) {
	t.Helper()
	got := make([]string, 0, len(lines))
	for _, line := range lines {
		dec := json.NewDecoder(strings.NewReader(line))
		var obj map[string]json.RawMessage
		if err := dec.Decode(&obj); err != nil {
			t.Errorf("stdout line is not a JSON object: %q: %v", line, err)
			continue
		}
		if _, err := dec.Token(); !errors.Is(err, io.EOF) {
			t.Errorf("stdout line holds more than one JSON value: %q", line)
		}
		if string(obj["jsonrpc"]) != `"2.0"` {
			t.Errorf("stdout line is not a JSON-RPC 2.0 frame: %q", line)
		}
		got = append(got, string(obj["id"]))
	}
	slices.Sort(got)
	want := slices.Sorted(slices.Values(ids))
	if !slices.Equal(got, want) {
		t.Errorf("stdout frames carry ids %q, want exactly one response to each of %q; stdout:\n%s",
			got, want, strings.Join(lines, "\n"))
	}
}

// requireResult fails the test unless r is a result, and decodes it into v.
func requireResult(t *testing.T, what string, r rpcResponse, v any) {
	t.Helper()
	if r.Error != nil {
		t.Fatalf("%s: error %d %q, want a result", what, r.Error.Code, r.Error.Message)
	}
	if err := json.Unmarshal(r.Result, v); err != nil {
		t.Fatalf("%s: decoding result %s: %v", what, r.Result, err)
	}
}

// requireError checks that r is a JSON-RPC error with code.
func requireError(t *testing.T, what string, r rpcResponse, code int) {
	t.Helper()
	if r.Error == nil {
		t.Errorf("%s: result %s, want error %d", what, r.Result, code)
		return
	}
	if r.Error.Code != code {
		t.Errorf("%s: error %d %q, want code %d", what, r.Error.Code, r.Error.Message, code)
	}
}

// toolsList is the part of a tools/list result the tests inspect.
type toolsList struct {
	Tools []struct {
		Name        string `json:"name"`
		InputSchema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"inputSchema"`
		OutputSchema json.RawMessage `json:"outputSchema"`
	} `json:"tools"`
}

// checkToolsList checks that list names the four tools, each with an
// output schema, that check_package's input schema declares staged, and
// that its output schema's success branch declares the module block.
func checkToolsList(t *testing.T, list toolsList) {
	t.Helper()
	names := make([]string, 0, len(list.Tools))
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
		if len(tool.OutputSchema) == 0 {
			t.Errorf("tools/list: %s has no outputSchema", tool.Name)
			continue
		}
		if tool.Name != "check_package" {
			continue
		}
		if tool.InputSchema.Properties["staged"] == nil {
			t.Errorf("tools/list: check_package inputSchema properties = %v, want staged", slices.Collect(maps.Keys(tool.InputSchema.Properties)))
		}
		var schema struct {
			OneOf []struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"oneOf"`
		}
		if err := json.Unmarshal(tool.OutputSchema, &schema); err != nil || len(schema.OneOf) == 0 ||
			schema.OneOf[0].Properties["module"] == nil || schema.OneOf[0].Properties["package_path"] == nil {
			t.Errorf("tools/list: check_package outputSchema = %s, want a success branch with package_path and module", tool.OutputSchema)
		}
	}
	slices.Sort(names)
	if want := []string{"assess_package", "check_package", "explain_metric", "rank_packages"}; !slices.Equal(names, want) {
		t.Errorf("tools/list names = %v, want %v", names, want)
	}
}

// callResult is the part of a tools/call result the tests inspect.
type callResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StructuredContent struct {
		PackagePath string `json:"package_path"`
		Passed      *bool  `json:"passed"`
		Module      *struct {
			PackagePath string `json:"package_path"`
			Passed      *bool  `json:"passed"`
		} `json:"module"`
	} `json:"structuredContent"`
	IsError    bool    `json:"isError"`
	ResultType *string `json:"resultType"`
}

// checkDegradedResult checks a check_package result on the degraded
// fixture's tested package: a gate failure, which is a result and not a
// tool error.
func checkDegradedResult(t *testing.T, res callResult) {
	t.Helper()
	if res.IsError {
		t.Errorf("check_package isError = true, want a gate failure result")
	}
	if got := res.StructuredContent.PackagePath; got != "tested" {
		t.Errorf("check_package structuredContent.package_path = %q, want tested", got)
	}
	if p := res.StructuredContent.Passed; p == nil || *p {
		t.Errorf("check_package structuredContent.passed = %v, want false", p)
	}
	if m := res.StructuredContent.Module; m == nil || m.PackagePath != "module" || m.Passed == nil {
		t.Errorf("check_package structuredContent.module = %+v, want the gated module row", m)
	}
	if len(res.Content) == 0 || res.Content[0].Type != "text" || !strings.HasPrefix(res.Content[0].Text, "FAILED") {
		t.Errorf("check_package content = %+v, want text starting with FAILED", res.Content)
	}
}

// protocolWorkspace copies the degraded fixture and the module it replaces
// into a temporary directory, writes the pristine fixture's baseline to
// another, and returns the fixture copy and the baseline path. The baseline
// is outside the working directory, so serve needs --allow-any-path.
func protocolWorkspace(t *testing.T) (dir, base string) {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(tmp, "fixture")
	copyTree(t, degradedDir, dir)
	copyTree(t, extmodDir, filepath.Join(tmp, "extmod"))
	return dir, fixtureBaseline(t)
}

// checkArgs returns the tools/call arguments that check the tested package
// against base.
func checkArgs(base string) string {
	var b bytes.Buffer
	_ = json.NewEncoder(&b).Encode(map[string]string{"path": "tested", "baseline_file": base})
	return strings.TrimSpace(b.String())
}

func TestServeProtocolEras(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs serve and loads Go packages")
	}
	t.Parallel()

	dir, base := protocolWorkspace(t)
	args := checkArgs(base)

	t.Run(modernVersion, func(t *testing.T) {
		t.Parallel()

		s := startStdioSession(t, dir)
		meta := requestMeta(modernVersion)

		var disc struct {
			SupportedVersions []string        `json:"supportedVersions"`
			Capabilities      json.RawMessage `json:"capabilities"`
			Meta              struct {
				ServerInfo struct {
					Name string `json:"name"`
				} `json:"io.modelcontextprotocol/serverInfo"`
			} `json:"_meta"`
			ResultType string `json:"resultType"`
		}
		requireResult(t, "server/discover", s.call(1, "server/discover", `{"_meta":`+meta+`}`), &disc)
		if !slices.Contains(disc.SupportedVersions, modernVersion) || !slices.Contains(disc.SupportedVersions, legacyVersion) {
			t.Errorf("server/discover supportedVersions = %v, want %s and %s", disc.SupportedVersions, modernVersion, legacyVersion)
		}
		if !strings.Contains(string(disc.Capabilities), `"tools"`) {
			t.Errorf("server/discover capabilities = %s, want tools", disc.Capabilities)
		}
		if disc.Meta.ServerInfo.Name != "astimate" || disc.ResultType != "complete" {
			t.Errorf("server/discover serverInfo.name = %q, resultType = %q; want astimate and complete",
				disc.Meta.ServerInfo.Name, disc.ResultType)
		}

		var list toolsList
		requireResult(t, "tools/list", s.call(2, "tools/list", `{"_meta":`+meta+`}`), &list)
		checkToolsList(t, list)

		var res callResult
		requireResult(t, "tools/call", s.call(3, "tools/call",
			`{"name":"check_package","arguments":`+args+`,"_meta":`+meta+`}`), &res)
		checkDegradedResult(t, res)
		if res.ResultType == nil || *res.ResultType != "complete" {
			t.Errorf("tools/call resultType = %v, want complete", res.ResultType)
		}

		requireError(t, "unknown tool", s.call(4, "tools/call",
			`{"name":"nope","arguments":{},"_meta":`+meta+`}`), codeInvalidParams)
		requireError(t, "unsupported version", s.call(5, "tools/list",
			`{"_meta":`+requestMeta("2099-01-01")+`}`), codeUnsupportedProtocolVersion)

		s.close()
	})

	t.Run("legacy", func(t *testing.T) {
		t.Parallel()

		s := startStdioSession(t, dir)

		var init struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		}
		requireResult(t, "initialize", s.call(1, "initialize", `{"protocolVersion":"`+legacyVersion+
			`","capabilities":{},"clientInfo":{"name":"test","version":"0"}}`), &init)
		if init.ProtocolVersion != legacyVersion || init.ServerInfo.Name != "astimate" {
			t.Errorf("initialize result = %+v, want protocolVersion %s and serverInfo.name astimate", init, legacyVersion)
		}
		s.send("", `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)

		var list toolsList
		requireResult(t, "tools/list", s.call(2, "tools/list", `{}`), &list)
		checkToolsList(t, list)

		var res callResult
		requireResult(t, "tools/call", s.call(3, "tools/call", `{"name":"check_package","arguments":`+args+`}`), &res)
		checkDegradedResult(t, res)
		if res.ResultType != nil {
			t.Errorf("tools/call resultType = %q, want none for a legacy client", *res.ResultType)
		}

		requireError(t, "unknown tool", s.call(4, "tools/call", `{"name":"nope","arguments":{}}`), codeInvalidParams)

		s.close()
	})
}
