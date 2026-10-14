package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestWriteHookNotes checks that notes add a why line and an open line
// under a violation, that its own line is unchanged, and that a nil notes
// writes exactly what WriteHook does.
func TestWriteHookNotes(t *testing.T) {
	t.Parallel()

	reasonOf := func(t *testing.T, notes FindingNotes) string {
		t.Helper()
		var out, warn bytes.Buffer
		if err := WriteHookNotes(&out, &warn, fixedCheck(), notes); err != nil {
			t.Fatal(err)
		}
		var got hookBlock
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("decoding %q: %v", out.String(), err)
		}
		return got.Reason
	}
	plain := reasonOf(t, nil)
	var out, warn bytes.Buffer
	if err := WriteHook(&out, &warn, fixedCheck()); err != nil {
		t.Fatal(err)
	}
	var want hookBlock
	if err := json.Unmarshal(out.Bytes(), &want); err != nil || plain != want.Reason {
		t.Errorf("nil notes wrote %q, want WriteHook's %q (%v)", plain, want.Reason, err)
	}

	notes := func(path, metric string) (string, string) {
		if metric != "dup_blocks" {
			return "", ""
		}
		return "why " + path, "file.go:7"
	}
	noted := reasonOf(t, notes)
	if strings.Count(noted, "\n      why: why ") != 1 || strings.Count(noted, "\n      open file.go:7\n") != 1 {
		t.Errorf("reason has the notes of dup_blocks other than once:\n%s", noted)
	}
	// Dropping the added lines leaves the plain reason, so every first line
	// reads as before.
	var kept []string
	for _, l := range strings.Split(noted, "\n") {
		if !strings.HasPrefix(l, "      ") {
			kept = append(kept, l)
		}
	}
	if got := strings.Join(kept, "\n"); got != plain {
		t.Errorf("reason without the notes = %q, want %q", got, plain)
	}
}
