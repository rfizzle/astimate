package detect

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/lang/golang"
)

// defaults returns the embedded default configuration.
func defaults(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// write creates each file of files, by slash path under root, with its
// content.
func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRegistry(t *testing.T) {
	t.Parallel()

	reg, err := Registry(defaults(t), "est", nil)
	if err != nil {
		t.Fatalf("Registry: %v", err)
	}
	if got, want := reg.Languages(), []string{"go", "typescript"}; !slices.Equal(got, want) {
		t.Errorf("languages = %q, want %q, Go first", got, want)
	}
}

// TestWarnings warns once for an override no extractor reports, and not
// for the shipped languages.
func TestWarnings(t *testing.T) {
	t.Parallel()

	top, _, _ := strings.Cut(string(config.Default()), "\nlanguages:\n")
	const section = "\nlanguages:\n  rust:\n    rebuild:\n      cocomo_a: 3\n  go: {}\n  typescript: {}\n"
	cfg, err := config.Parse([]byte(top + "\n" + section))
	if err != nil {
		t.Fatalf("parsing config: %v", err)
	}
	reg, err := Registry(cfg, "est", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"languages.rust: unknown language; known: go, typescript"}
	if got := Warnings(cfg, reg); !slices.Equal(got, want) {
		t.Errorf("Warnings = %q, want %q", got, want)
	}
	if got := Warnings(defaults(t), reg); len(got) != 0 {
		t.Errorf("default Warnings = %q, want none", got)
	}
}

// TestModule walks up to the nearest detected root, prefers Go over
// TypeScript at one root with a warning, and names go.mod outside any
// module.
func TestModule(t *testing.T) {
	t.Parallel()

	reg, err := Registry(defaults(t), "est", nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		files    map[string]string
		dir      string
		wantLang string
		wantWarn bool
	}{
		{name: "go below the root", files: map[string]string{"go.mod": "module example.com/m\n", "a/b/x.go": "package b\n"},
			dir: "a/b", wantLang: "go"},
		{name: "typescript", files: map[string]string{"package.json": "{}\n", "src/x.ts": "export {}\n"},
			dir: "src", wantLang: "typescript"},
		{name: "both at one root", files: map[string]string{"go.mod": "module example.com/m\n", "package.json": "{}\n"},
			dir: ".", wantLang: "go", wantWarn: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			write(t, root, tt.files)
			var buf bytes.Buffer
			got, ext, err := Module(filepath.Join(root, filepath.FromSlash(tt.dir)), reg, slog.New(slog.NewTextHandler(&buf, nil)))
			if err != nil {
				t.Fatalf("Module: %v", err)
			}
			if got != root || ext.Language() != tt.wantLang {
				t.Errorf("Module = (%s, %s), want (%s, %s)", got, ext.Language(), root, tt.wantLang)
			}
			if warned := strings.Contains(buf.String(), "several languages detected"); warned != tt.wantWarn {
				t.Errorf("warned = %v, want %v; log:\n%s", warned, tt.wantWarn, &buf)
			}
		})
	}

	t.Run("no module", func(t *testing.T) {
		t.Parallel()

		_, _, err := Module(t.TempDir(), reg, nil)
		if err == nil {
			t.Skip("a directory above the temporary directory is a module")
		}
		if !errors.Is(err, golang.ErrNoModule) || !errors.Is(err, ErrNoLanguage) {
			t.Errorf("Module = %v, want golang.ErrNoModule and ErrNoLanguage", err)
		}
	})
}

func TestModulePathFunc(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write(t, root, map[string]string{"go.mod": "module example.com/m\n"})
	read := ModulePathFunc("go")
	if read == nil {
		t.Fatal("ModulePathFunc(go) = nil, want golang.ModulePath")
	}
	if got, err := read(root); err != nil || got != "example.com/m" {
		t.Errorf("module path = (%q, %v), want example.com/m", got, err)
	}
	if ModulePathFunc("typescript") != nil {
		t.Error("ModulePathFunc(typescript) is set, want nil: its identifiers are directories")
	}
}
