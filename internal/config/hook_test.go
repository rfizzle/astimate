package config

import (
	"strings"
	"testing"
)

// TestParseHook checks the hook section: absent, empty or commented out as
// the default ships it, the block log is off; a path turns it on; a path
// naming no file, an unknown key and the section under a languages
// override are errors.
func TestParseHook(t *testing.T) {
	t.Parallel()

	const anchor = "# hook:\n#   block_log: .astimate/blocks.jsonl\n"
	tests := []struct {
		name    string
		repl    string
		want    string
		wantErr string
	}{
		{name: "default", repl: anchor, want: ""},
		{name: "empty section", repl: "hook: {}\n", want: ""},
		{name: "relative path", repl: "hook:\n  block_log: .astimate/blocks.jsonl\n", want: ".astimate/blocks.jsonl"},
		{name: "absolute path", repl: "hook:\n  block_log: /var/log/blocks.jsonl\n", want: "/var/log/blocks.jsonl"},
		{name: "explicitly off", repl: "hook:\n  block_log: \"\"\n", want: ""},
		{name: "directory", repl: "hook:\n  block_log: .astimate/\n", wantErr: `hook.block_log ".astimate/"`},
		{name: "dot dot", repl: "hook:\n  block_log: ..\n", wantErr: `hook.block_log ".."`},
		{name: "blanks", repl: "hook:\n  block_log: \" blocks.jsonl\"\n", wantErr: `hook.block_log " blocks.jsonl"`},
		{name: "unknown key", repl: "hook:\n  log: blocks.jsonl\n", wantErr: "log"},
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
			if cfg.Hook.BlockLog != tt.want {
				t.Errorf("Hook.BlockLog = %q, want %q", cfg.Hook.BlockLog, tt.want)
			}
		})
	}

	t.Run("languages override", func(t *testing.T) {
		t.Parallel()

		data := string(Default()) + "\n" // the default ends with its languages section
		data = strings.Replace(data, "\nlanguages:\n  typescript:\n", "\nlanguages:\n  typescript:\n    hook:\n      block_log: x.jsonl\n", 1)
		if _, err := Parse([]byte(data)); err == nil || !strings.Contains(err.Error(), "hook") {
			t.Errorf("Parse() error = %v, want the hook key rejected under languages", err)
		}
	})
}
