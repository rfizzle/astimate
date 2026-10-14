package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hookSampleMarkers fence the sample of docs/claude-code-hook.md that
// TestHookGolden keeps equal to the hook's reason on the degraded fixture.
const (
	hookSampleBegin = "<!-- hook-sample:begin -->\n"
	hookSampleEnd   = "<!-- hook-sample:end -->"
)

// TestHookGolden checks the reason check --format hook hands the agent on
// the degraded fixture against testdata/check_degraded_hook.txt, and that
// the sample in docs/claude-code-hook.md is that reason. UPDATE_GOLDEN=1
// regenerates both.
func TestHookGolden(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	var out, errOut bytes.Buffer
	args := []string{"check", "--all", "--baseline", writeHookBaseline(t), "--config", pinnedConfigFile(t), "--format", "hook", hookTestdata("fixture-degraded")}
	if got := run(args, &out, &errOut); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, errOut.String())
	}
	var block struct{ Reason string }
	if err := json.Unmarshal(out.Bytes(), &block); err != nil || block.Reason == "" {
		t.Fatalf("hook stdout = %q, want a block with a reason (%v)", out.String(), err)
	}
	compareGolden(t, "check_degraded_hook.txt", block.Reason)

	doc := filepath.Join("..", "..", "docs", "claude-code-hook.md")
	data, err := os.ReadFile(doc)
	if err != nil {
		t.Fatal(err)
	}
	head, rest, ok := strings.Cut(string(data), hookSampleBegin)
	_, tail, ok2 := strings.Cut(rest, hookSampleEnd)
	if !ok || !ok2 {
		t.Fatalf("%s has no hook-sample markers", doc)
	}
	want := hookSampleBegin + "```\n" + block.Reason + "```\n" + hookSampleEnd
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(doc, []byte(head+want+tail), 0o644); err != nil {
			t.Fatalf("updating %s: %v", doc, err)
		}
		return
	}
	if !strings.HasPrefix(rest, strings.TrimPrefix(want, hookSampleBegin)) {
		t.Errorf("the sample in %s differs from the hook's reason (regenerate with UPDATE_GOLDEN=1); the reason is:\n%s", doc, block.Reason)
	}
}
