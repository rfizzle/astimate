package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/engine"
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

// withoutLanguages returns a configuration file cut before its languages
// section, the last key of the embedded default, so a test can append a
// languages section of its own; the whole file when it has none.
func withoutLanguages(data []byte) []byte {
	top, _, _ := strings.Cut(string(data), "\nlanguages:\n")
	return []byte(top + "\n")
}

// TestConfigInitValidates checks the written file loads without warnings
// and resolves Go to the embedded default's top level and TypeScript to its
// shipped override.
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
	if eff := cfg.ForLanguage("go"); eff.Version != cfg.Version || eff.Rebuild != cfg.Rebuild ||
		len(eff.Thresholds) != len(cfg.Thresholds) {
		t.Errorf("ForLanguage(go) = %+v, want the top level unchanged", eff)
	}
	if eff := cfg.ForLanguage("typescript"); eff.Version != cfg.Version+"+typescript" || eff.Rebuild != cfg.Rebuild ||
		len(eff.Thresholds) != len(cfg.Thresholds) {
		t.Errorf("ForLanguage(typescript) = %+v, want the override's version, the top-level rebuild and one rule per metric", eff)
	}
	if warns, err := engine.LanguageWarnings(cfg); err != nil || len(warns) != 0 {
		t.Errorf("LanguageWarnings = %q, %v, want none", warns, err)
	}

	// The registry, not the config package, judges override ids: the
	// written file with overrides appended warns only on the unknown one.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written config: %v", err)
	}
	withOverrides, err := config.Parse(append(withoutLanguages(data), "\nlanguages:\n  rust: {}\n  go: {}\n  typescript: {}\n"...))
	if err != nil {
		t.Fatalf("parsing config with overrides: %v", err)
	}
	warns, err := engine.LanguageWarnings(withOverrides)
	want := []string{"languages.rust: unknown language; known: go, typescript"}
	if err != nil || !slices.Equal(warns, want) {
		t.Errorf("LanguageWarnings = %q, %v, want %q", warns, err, want)
	}
}
