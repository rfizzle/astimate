package config

import (
	"strings"
	"testing"
)

// TestParseChurn checks the churn section: absent or partial it takes the
// default window, 200 commits; 0 turns churn off; a negative window and an
// unknown key are errors.
func TestParseChurn(t *testing.T) {
	t.Parallel()

	const section = "churn:\n" +
		"  # The window: the last first-parent commits from HEAD whose changed files\n" +
		"  # churn_commits and churn_authors count, read with one git log per run.\n" +
		"  # 0 turns churn off, and the two metrics are null.\n" +
		"  commits: 200\n"
	tests := []struct {
		name    string
		repl    string
		want    int
		wantErr string
	}{
		{name: "default", repl: section, want: 200},
		{name: "absent section", repl: "", want: 200},
		{name: "empty section", repl: "churn: {}\n", want: 200},
		{name: "smaller window", repl: "churn:\n  commits: 2\n", want: 2},
		{name: "off", repl: "churn:\n  commits: 0\n", want: 0},
		{name: "negative", repl: "churn:\n  commits: -1\n", wantErr: "churn.commits must be >= 0, got -1"},
		{name: "unknown key", repl: "churn:\n  window: 10\n", wantErr: "window"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := Parse([]byte(replace(t, section, tt.repl)))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Parse() error = %v, want error containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if cfg.Churn.Commits != tt.want {
				t.Errorf("Churn.Commits = %d, want %d", cfg.Churn.Commits, tt.want)
			}
		})
	}
}
