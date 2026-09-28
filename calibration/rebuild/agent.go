package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// defaultAgentName names the default agent in the output directory and in
// the config_version the fit ships (rebuild-<date>-claude-code).
const defaultAgentName = "claude-code"

// customAgentName names an agent given with --agent when --agent-name is
// not set.
const customAgentName = "custom"

// defaultModel is the model the default agent command asks for: the model
// Claude Code was configured with on the host the runner was written on.
// --model overrides it; the row records both the request and the models
// the agent reported using.
const defaultModel = "claude-fable-5-1"

// defaultAgentTemplate is the Claude Code print-mode invocation a live run
// executes, through sh -c with the module root as its working directory.
// The prompt comes from the prompt file; --max-turns caps the agentic turns
// at the experiment's turn_cap; stream-json output (which print mode only
// writes with --verbose) carries every tool call and ends with the result
// object holding usage, cost, turns and duration. dontAsk denies every
// tool not listed, so the agent can read and edit files and run the go
// commands it needs to test its work, but cannot run git (the clone's
// history holds the original implementation), fetch from the web or start
// arbitrary programs; the disallowed list, which wins over the allowed
// one, also refuses go commands that name git, run a program through
// -toolexec, -exec or -vettool, swap sources with -overlay, or reach into
// the module cache. --safe-mode and --strict-mcp-config keep the user's
// CLAUDE.md, skills, plugins, hooks and MCP servers out of the run, and
// --no-session-persistence leaves no session behind.
const defaultAgentTemplate = `claude -p "$(cat {prompt_file})"` +
	` --output-format stream-json --verbose` +
	` --model {model} --max-turns {turn_cap}` +
	` --permission-mode dontAsk` +
	` --allowedTools 'Read,Edit,Write,Glob,Grep,Bash(go build *),Bash(go test *),Bash(go vet *),Bash(go doc *),Bash(go list *),Bash(gofmt *)'` +
	` --disallowedTools 'Bash(*git *),Bash(*-toolexec*),Bash(*-exec*),Bash(*-vettool*),Bash(*-overlay*),Bash(*pkg/mod*)'` +
	` --safe-mode --strict-mcp-config --no-session-persistence`

// placeholders are the names an agent template may use, each written as
// {name} and replaced by its value, shell-quoted.
func placeholders() []string {
	return []string{"root", "dir", "package", "prompt_file", "turn_cap", "model"}
}

// errTemplate is wrapped by every template error.
var errTemplate = errors.New("invalid agent template")

// templateNames returns the {name} placeholders in tmpl, in order of
// appearance. A brace preceded by '$' is shell parameter expansion, not a
// placeholder.
func templateNames(tmpl string) []string {
	var names []string
	for i := 0; i < len(tmpl); i++ {
		if tmpl[i] != '{' || (i > 0 && tmpl[i-1] == '$') {
			continue
		}
		end := strings.IndexByte(tmpl[i:], '}')
		if end < 0 {
			break
		}
		name := tmpl[i+1 : i+end]
		if name != "" && strings.Trim(name, "abcdefghijklmnopqrstuvwxyz_") == "" {
			names = append(names, name)
		}
		i += end
	}
	return names
}

// checkTemplate reports an unknown placeholder in tmpl.
func checkTemplate(tmpl string) error {
	if strings.TrimSpace(tmpl) == "" {
		return fmt.Errorf("%w: empty", errTemplate)
	}
	for _, n := range templateNames(tmpl) {
		if !slices.Contains(placeholders(), n) {
			return fmt.Errorf("%w: unknown placeholder {%s}; known: {%s}", errTemplate, n, strings.Join(placeholders(), "}, {"))
		}
	}
	return nil
}

// renderTemplate replaces each placeholder of tmpl with its value from
// vals, single-quoted for sh.
func renderTemplate(tmpl string, vals map[string]string) string {
	pairs := make([]string, 0, 2*len(vals))
	for _, n := range placeholders() {
		if v, ok := vals[n]; ok {
			pairs = append(pairs, "{"+n+"}", shellQuote(v))
		}
	}
	// "${" must survive; protect it from a placeholder of the same name.
	const guard = "\x00"
	s := strings.ReplaceAll(tmpl, "${", guard)
	s = strings.NewReplacer(pairs...).Replace(s)
	return strings.ReplaceAll(s, guard, "${")
}

// shellQuote quotes s as one sh word.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./=:@,+") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// buildPrompt is the fixed prompt of every run.
func buildPrompt(e *Experiment, root string, env []string) string {
	var b strings.Builder
	b.WriteString("The Go package " + e.Package + " in the module at " + root +
		" (directory " + ownPattern(e.Dir) + ") has had its implementation removed: every function and method body " +
		"in its non-test .go files panics with \"not implemented\". Its types, constants, variables, " +
		"signatures and doc comments are intact, and so are all test files.\n\n")
	if e.HasTests {
		b.WriteString("Reimplement the package so that `go test " + ownPattern(e.Dir) + "` passes. ")
	} else {
		b.WriteString("The package has no tests of its own. Reimplement it so that the tests of the packages " +
			"that import it pass: `go test " + strings.Join(e.Oracle.Test, " ") + "`. ")
	}
	b.WriteString("Do not edit test files. Do not touch other packages: change only the non-test .go files in " +
		ownPattern(e.Dir) + ".\n\nYou are done when this command succeeds from the module root:\n\n    " +
		strings.Join(env, " ") + " " + e.Oracle.Command() + "\n")
	return b.String()
}

// Measured is what a run's agent reported about itself, read from its
// output. A field the output did not carry is null and named in Missing.
type Measured struct {
	// InputTokens are the uncached input tokens.
	InputTokens *int64 `json:"input_tokens"`
	// OutputTokens are the generated tokens.
	OutputTokens *int64 `json:"output_tokens"`
	// CacheReadTokens are the input tokens read from the prompt cache.
	CacheReadTokens *int64 `json:"cache_read_tokens"`
	// CacheWriteTokens are the input tokens written to the prompt cache.
	CacheWriteTokens *int64 `json:"cache_write_tokens"`
	// TokenSource is where the token counts came from: "modelUsage" (summed
	// over every model the session used, subagents included) or "usage"
	// (the result's own usage object).
	TokenSource *string `json:"token_source"`
	// CostUSD is the session's total cost as the agent reported it.
	CostUSD *float64 `json:"cost_usd"`
	// Turns is the number of agentic turns.
	Turns *int `json:"turns"`
	// ToolCalls counts the tool_use blocks in the streamed messages; null
	// for output that is not streamed.
	ToolCalls *int `json:"tool_calls"`
	// DurationMS is the session's duration as the agent reported it.
	DurationMS *int64 `json:"duration_ms"`
	// APIDurationMS is the time the agent reported waiting on the API.
	APIDurationMS *int64 `json:"api_duration_ms"`
	// SessionID identifies the agent session.
	SessionID *string `json:"session_id"`
	// ResultSubtype is the result's subtype: success, error_max_turns,
	// error_during_execution.
	ResultSubtype *string `json:"result_subtype"`
	// IsError is the result's is_error flag.
	IsError *bool `json:"is_error"`
	// TurnCapHit is true when the result says the turn cap stopped the run
	// or the turns reached the cap.
	TurnCapHit *bool `json:"turn_cap_hit"`
	// Models are the models the session reported using, sorted.
	Models []string `json:"models"`
	// Missing names the fields above that the output did not provide.
	Missing []string `json:"missing"`
}

// streamEvent is one JSON object of Claude Code's print-mode output: the
// result object of --output-format json, or any line of stream-json.
// Unknown fields are ignored.
type streamEvent struct {
	Type          string                     `json:"type"`
	Subtype       *string                    `json:"subtype"`
	IsError       *bool                      `json:"is_error"`
	DurationMS    *int64                     `json:"duration_ms"`
	DurationAPIMS *int64                     `json:"duration_api_ms"`
	NumTurns      *int                       `json:"num_turns"`
	SessionID     *string                    `json:"session_id"`
	TotalCostUSD  *float64                   `json:"total_cost_usd"`
	Usage         *usageObject               `json:"usage"`
	ModelUsage    map[string]json.RawMessage `json:"modelUsage"`
	Message       json.RawMessage            `json:"message"`
}

// usageObject is the result's usage.
type usageObject struct {
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
}

// modelUsageObject is one model's entry of the result's modelUsage.
type modelUsageObject struct {
	InputTokens              *int64 `json:"inputTokens"`
	OutputTokens             *int64 `json:"outputTokens"`
	CacheReadInputTokens     *int64 `json:"cacheReadInputTokens"`
	CacheCreationInputTokens *int64 `json:"cacheCreationInputTokens"`
}

// contentBlock is one block of a streamed message's content.
type contentBlock struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// ParseAgentOutput reads the measurements from an agent's standard output:
// Claude Code's --output-format json (one result object) or stream-json
// (one object per line, ending with the result). Lines that are not JSON
// objects are skipped, unknown fields ignored, and anything the output did
// not carry is left null and named in Missing. turnCap decides TurnCapHit
// when the result gives the turns but not the reason it stopped.
func ParseAgentOutput(out []byte, turnCap int) Measured {
	var m Measured
	var result *streamEvent
	streamed := false
	toolIDs := map[string]bool{}
	toolCalls := 0
	for line := range bytes.Lines(out) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev streamEvent
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		switch ev.Type {
		case "result":
			result = &ev
		case "assistant":
			streamed = true
			toolCalls += countToolUses(ev.Message, toolIDs)
		case "system", "user":
			streamed = true
		}
	}
	if result == nil {
		// --output-format json may be pretty-printed over several lines.
		var ev streamEvent
		if json.Unmarshal(bytes.TrimSpace(out), &ev) == nil && ev.Type == "result" {
			result = &ev
		}
	}
	if streamed {
		m.ToolCalls = &toolCalls
	}
	if result != nil {
		fillResult(&m, result)
	}
	switch {
	case m.ResultSubtype != nil && *m.ResultSubtype == "error_max_turns":
		m.TurnCapHit = new(true)
	case m.Turns != nil:
		m.TurnCapHit = new(turnCap > 0 && *m.Turns >= turnCap)
	case m.ResultSubtype != nil:
		m.TurnCapHit = new(false)
	}
	m.Missing = missingFields(&m)
	return m
}

// countToolUses counts the tool_use blocks of a streamed message that have
// not been counted before; a block without an id always counts.
func countToolUses(msg json.RawMessage, seen map[string]bool) int {
	var body struct {
		Content json.RawMessage `json:"content"`
	}
	if len(msg) == 0 || json.Unmarshal(msg, &body) != nil {
		return 0
	}
	var blocks []contentBlock
	if json.Unmarshal(body.Content, &blocks) != nil {
		return 0
	}
	n := 0
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		if b.ID != "" {
			if seen[b.ID] {
				continue
			}
			seen[b.ID] = true
		}
		n++
	}
	return n
}

// fillResult copies the result object's fields into m. Token counts come
// from modelUsage when it is present, since it covers every model the
// session used, else from usage.
func fillResult(m *Measured, r *streamEvent) {
	m.CostUSD, m.Turns, m.SessionID = r.TotalCostUSD, r.NumTurns, r.SessionID
	m.DurationMS, m.APIDurationMS = r.DurationMS, r.DurationAPIMS
	m.ResultSubtype, m.IsError = r.Subtype, r.IsError
	if len(r.ModelUsage) > 0 {
		var in, out, read, write *int64
		for name, raw := range r.ModelUsage {
			m.Models = append(m.Models, name)
			var mu modelUsageObject
			if json.Unmarshal(raw, &mu) != nil {
				continue
			}
			in, out = addOpt(in, mu.InputTokens), addOpt(out, mu.OutputTokens)
			read, write = addOpt(read, mu.CacheReadInputTokens), addOpt(write, mu.CacheCreationInputTokens)
		}
		slices.Sort(m.Models)
		if in != nil || out != nil {
			m.InputTokens, m.OutputTokens, m.CacheReadTokens, m.CacheWriteTokens = in, out, read, write
			m.TokenSource = new("modelUsage")
			return
		}
	}
	if u := r.Usage; u != nil {
		m.InputTokens, m.OutputTokens = u.InputTokens, u.OutputTokens
		m.CacheReadTokens, m.CacheWriteTokens = u.CacheReadInputTokens, u.CacheCreationInputTokens
		m.TokenSource = new("usage")
	}
}

// addOpt adds b to a, where nil is absent: the sum is nil only when both
// are.
func addOpt(a, b *int64) *int64 {
	switch {
	case b == nil:
		return a
	case a == nil:
		return new(*b)
	}
	return new(*a + *b)
}

// missingFields names the null measurements of m, by their JSON names.
func missingFields(m *Measured) []string {
	missing := []string{}
	add := func(absent bool, name string) {
		if absent {
			missing = append(missing, name)
		}
	}
	add(m.InputTokens == nil, "input_tokens")
	add(m.OutputTokens == nil, "output_tokens")
	add(m.CacheReadTokens == nil, "cache_read_tokens")
	add(m.CacheWriteTokens == nil, "cache_write_tokens")
	add(m.TokenSource == nil, "token_source")
	add(m.CostUSD == nil, "cost_usd")
	add(m.Turns == nil, "turns")
	add(m.ToolCalls == nil, "tool_calls")
	add(m.DurationMS == nil, "duration_ms")
	add(m.APIDurationMS == nil, "api_duration_ms")
	add(m.SessionID == nil, "session_id")
	add(m.ResultSubtype == nil, "result_subtype")
	add(m.IsError == nil, "is_error")
	add(m.TurnCapHit == nil, "turn_cap_hit")
	add(m.Models == nil, "models")
	return missing
}

// itoa formats n in decimal.
func itoa(n int) string { return strconv.Itoa(n) }
