package narrate

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
)

// keys returns the JSON keys v encodes to, sorted.
func keys(t *testing.T, v any) []string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return slices.Sorted(maps.Keys(m))
}

func TestWriteJSON(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	if err := WriteJSON(&b, &Module{Name: "<a & b>"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "\n  \"name\": \"<a & b>\",\n") {
		t.Errorf("WriteJSON = %s, want indented and unescaped", b.String())
	}
}

// TestSchema pins the keys of the module report SPEC.md 10.3 states;
// the omitted-when-empty keys are set.
func TestSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    any
		want []string
	}{
		{"module", Module{Head: "h", Change: &Change{}}, []string{"astimate_version", "change", "churn_window",
			"config_version", "counts", "glossary", "head", "headline", "hotspots", "language", "lead", "limits",
			"module_path", "name", "packages"}},
		{"package", Package{Role: "library", Changes: "c"}, []string{"band", "ceilings", "changes", "churn", "complexity",
			"copies", "facts", "fan_in", "hotspot", "over", "path", "rebuild", "role", "story", "tests", "tier", "todo"}},
		{"rebuild", Rebuild{}, []string{"agent_passes", "drivers", "human_days", "human_short", "human_time",
			"rebuild_tokens", "say", "sittings", "sittings_text"}},
		{"copies", Copies{Longest: "l"}, []string{"blocks", "chunks", "code_blocks", "level", "lines", "longest", "pct", "say"}},
		{"complexity", Complexity{Say: "s"}, []string{"buckets", "cognitive_p90", "cyclomatic_p90", "functions",
			"hardest", "nesting", "say"}},
		{"change", Change{BaseCommit: "c", Deleted: []string{"d"}}, []string{"base", "base_commit", "changed", "cross",
			"deleted", "failed", "failing", "headline", "lead", "passed", "passed_paths", "passed_summary", "unchanged",
			"verdict", "violations", "warning_groups", "warnings"}},
		{"finding", Finding{Moved: "m", MovedShort: "s", Location: nil}, []string{"allowed", "base", "do", "head", "label",
			"limit", "metric", "moved", "moved_short", "over_by", "why"}},
	}
	for _, tt := range tests {
		if got := keys(t, tt.v); !slices.Equal(got, tt.want) {
			t.Errorf("%s keys = %q, want %q", tt.name, got, tt.want)
		}
	}
}
