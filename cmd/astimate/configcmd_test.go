package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
)

func TestConfigInitMatchesDefault(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "astimate.yaml")
	var stdout, stderr bytes.Buffer
	if got := run([]string{"config", "init", "--out", path}, &stdout, &stderr); got != exitOK {
		t.Fatalf("config init exit code = %d, want %d; stderr = %q", got, exitOK, stderr.String())
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written config: %v", err)
	}
	if !bytes.Equal(got, config.Default()) {
		t.Error("config init output differs from config.Default()")
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat written config: %v", err)
	}
	if perm := fi.Mode().Perm(); perm&^0o644 != 0 {
		t.Errorf("config file mode = %o, want at most 0644", perm)
	}
}

func TestConfigInitOverwrite(t *testing.T) {
	t.Parallel()

	const existing = "existing: true\n"
	tests := []struct {
		name        string
		force       bool
		wantCode    int
		wantContent []byte
	}{
		{name: "refuses without force", wantCode: exitUsage, wantContent: []byte(existing)},
		{name: "overwrites with force", force: true, wantCode: exitOK, wantContent: config.Default()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "astimate.yaml")
			if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
				t.Fatalf("seeding existing file: %v", err)
			}

			args := []string{"config", "init", "--out", path}
			if tt.force {
				args = append(args, "--force")
			}
			var stdout, stderr bytes.Buffer
			if got := run(args, &stdout, &stderr); got != tt.wantCode {
				t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, tt.wantCode, stderr.String())
			}
			if !tt.force {
				msg := stderr.String()
				if !strings.Contains(msg, path) || !strings.Contains(msg, "--force") {
					t.Errorf("refusal stderr = %q, want it to name %s and mention --force", msg, path)
				}
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading config: %v", err)
			}
			if !bytes.Equal(got, tt.wantContent) {
				t.Errorf("file content after run = %q, want %q", got, tt.wantContent)
			}
		})
	}
}

func TestConfigInitSecondRunRefuses(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "astimate.yaml")
	args := []string{"config", "init", "--out", path}
	var stdout, stderr bytes.Buffer
	if got := run(args, &stdout, &stderr); got != exitOK {
		t.Fatalf("first config init exit code = %d, want %d", got, exitOK)
	}
	stderr.Reset()
	if got := run(args, &stdout, &stderr); got != exitUsage {
		t.Fatalf("second config init exit code = %d, want %d", got, exitUsage)
	}
	if !strings.Contains(stderr.String(), path) {
		t.Errorf("second config init stderr = %q, want it to name %s", stderr.String(), path)
	}
}

func TestConfigInitBadArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "unknown flag", args: []string{"config", "init", "--bogus"}},
		{name: "positional argument", args: []string{"config", "init", "extra"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if got := run(tt.args, &stdout, &stderr); got != exitUsage {
				t.Errorf("run(%q) exit code = %d, want %d", tt.args, got, exitUsage)
			}
		})
	}
}

// TestConfigInitValidates checks the written file loads without warnings
// and, carrying no language override, resolves every language to the
// embedded default's top level.
func TestConfigInitValidates(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "astimate.yaml")
	var stdout, stderr bytes.Buffer
	if got := run([]string{"config", "init", "--out", path}, &stdout, &stderr); got != exitOK {
		t.Fatalf("config init exit code = %d, want %d; stderr = %q", got, exitOK, stderr.String())
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading written config: %v", err)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("Warnings = %q, want none", cfg.Warnings)
	}
	for _, lang := range []string{"go", "typescript"} {
		eff := cfg.ForLanguage(lang)
		if eff.Version != cfg.Version || eff.Rebuild != cfg.Rebuild || len(eff.Thresholds) != len(cfg.Thresholds) {
			t.Errorf("ForLanguage(%q) = %+v, want the top level unchanged", lang, eff)
		}
	}
}
