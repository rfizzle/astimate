package config

import (
	"slices"
	"strings"
	"testing"
)

// TestParseExclude checks the exclude key: absent it excludes nothing (the
// default file carries it commented out), a list keeps its patterns in
// order, and a malformed pattern or a key of the wrong shape is an error
// naming it.
func TestParseExclude(t *testing.T) {
	t.Parallel()

	const anchor = "# exclude:\n#   - \"internal/gen/**\"\n"
	tests := []struct {
		name    string
		repl    string
		want    []string
		wantErr string
	}{
		{name: "default", repl: anchor},
		{name: "empty list", repl: "exclude: []\n"},
		{name: "patterns", repl: "exclude:\n  - \"internal/gen/**\"\n  - \"**/*_gen.go\"\n",
			want: []string{"internal/gen/**", "**/*_gen.go"}},
		{name: "malformed class", repl: "exclude:\n  - \"ok/**\"\n  - \"gen/[a\"\n", wantErr: `exclude[1] "gen/[a"`},
		{name: "absolute", repl: "exclude: [\"/abs/**\"]\n", wantErr: `exclude[0] "/abs/**"`},
		{name: "double star in segment", repl: "exclude: [\"a/b**\"]\n", wantErr: `exclude[0] "a/b**": ** must be a whole path segment`},
		{name: "not a list", repl: "exclude: internal/gen\n", wantErr: "into []string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := Parse([]byte(replace(t, anchor, tt.repl)))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Parse() error = %v, want error containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if got := cfg.Exclude.Patterns(); !slices.Equal(got, tt.want) {
				t.Errorf("Exclude.Patterns() = %q, want %q", got, tt.want)
			}
		})
	}
}
