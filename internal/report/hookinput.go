package report

import (
	"encoding/json"
	"io"
)

// MaxHookInput bounds how much of the Claude Code Stop hook input
// StopHookActive reads.
const MaxHookInput = 64 << 10

// StopHookActive reports whether the Claude Code Stop hook input in r has
// stop_hook_active set to true (SPEC.md 8.5). A nil reader, a read error
// and missing or malformed input all report false, so the check runs as
// usual.
func StopHookActive(r io.Reader) bool {
	if r == nil {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxHookInput))
	if err != nil {
		return false
	}
	var in struct {
		StopHookActive bool `json:"stop_hook_active"`
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return false
	}
	return in.StopHookActive
}
