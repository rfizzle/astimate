package engine

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
	"github.com/rfizzle/astimate/internal/score"
)

// fixtureDir is the fixture module, relative to this package's directory.
const fixtureDir = "../../testdata/go/fixture"

func TestPackagePaths(t *testing.T) {
	t.Parallel()

	root := filepath.FromSlash("/src/mod")
	tests := []struct {
		name           string
		dir            string
		wantPkgPath    string
		wantImportPath string
	}{
		{name: "module root", dir: root, wantPkgPath: ".", wantImportPath: "example.com/mod"},
		{name: "child", dir: filepath.Join(root, "hub"), wantPkgPath: "hub", wantImportPath: "example.com/mod/hub"},
		{name: "nested", dir: filepath.Join(root, "internal", "billing"),
			wantPkgPath: "internal/billing", wantImportPath: "example.com/mod/internal/billing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pkgPath, importPath, err := packagePaths(root, tt.dir, "example.com/mod")
			if err != nil {
				t.Fatalf("packagePaths: %v", err)
			}
			if pkgPath != tt.wantPkgPath || importPath != tt.wantImportPath {
				t.Errorf("packagePaths(%q) = (%q, %q), want (%q, %q)",
					tt.dir, pkgPath, importPath, tt.wantPkgPath, tt.wantImportPath)
			}
		})
	}
}

func TestLoadTarget(t *testing.T) {
	t.Parallel()

	absFixture, err := filepath.Abs(fixtureDir)
	if err != nil {
		t.Fatalf("resolving fixture: %v", err)
	}
	tests := []struct {
		name           string
		dir            string
		wantPkgPath    string
		wantImportPath string
	}{
		{name: "relative package dir", dir: filepath.Join(fixtureDir, "hub"),
			wantPkgPath: "hub", wantImportPath: "example.com/fixture/hub"},
		{name: "module root", dir: fixtureDir, wantPkgPath: ".", wantImportPath: "example.com/fixture"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tg, err := LoadTarget(tt.dir, TargetOptions{Tokenizer: TokenizerEst})
			if err != nil {
				t.Fatalf("LoadTarget(%q): %v", tt.dir, err)
			}
			if tg.Mod.Root != absFixture || tg.Mod.ModulePath != "example.com/fixture" {
				t.Errorf("module = (%q, %q), want (%q, example.com/fixture)",
					tg.Mod.Root, tg.Mod.ModulePath, absFixture)
			}
			if tg.Dir != tt.wantPkgPath || tg.ImportPath != tt.wantImportPath {
				t.Errorf("paths = (%q, %q), want (%q, %q)",
					tg.Dir, tg.ImportPath, tt.wantPkgPath, tt.wantImportPath)
			}
		})
	}

	t.Run("non-module", func(t *testing.T) {
		t.Parallel()

		_, err := LoadTarget(t.TempDir(), TargetOptions{Tokenizer: TokenizerEst})
		if !errors.Is(err, golang.ErrNoModule) {
			t.Errorf("LoadTarget error = %v, want it to wrap golang.ErrNoModule", err)
		}
	})
}

// TestDupesDuplicationSuggestionNamesLocation checks the path check takes: the
// names assess resolves feed the duplication template, which cites the first
// occurrence of the first block.
func TestDupesDuplicationSuggestionNamesLocation(t *testing.T) {
	t.Parallel()

	tg, err := LoadTarget(filepath.Join(fixtureDir, "dupes"), TargetOptions{Tokenizer: TokenizerEst})
	if err != nil {
		t.Fatalf("LoadTarget: %v", err)
	}
	m, err := tg.Ext.Extract(t.Context(), tg.Mod, tg.ImportPath)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	names, err := Names(t.Context(), tg, tg.ImportPath)
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	got := score.MetricSuggestion("dup_blocks", float64(m.DupBlocks), m, names)
	want := "1 duplicate block covers 80.6% of lines; extract shared helpers, starting with dupes.go:9-26."
	if got != want {
		t.Errorf("dup_blocks suggestion = %q, want %q", got, want)
	}
}

func TestSuggestionNames(t *testing.T) {
	t.Parallel()

	const root = "/fake/module"
	pkgs := map[string]metrics.RawMetrics{"p": {UntestedExports: 1, DupBlocks: 1}}
	cross := []metrics.CrossBlock{{Occurrences: []metrics.Occurrence{
		{Package: "p", File: "p/p.go", StartLine: 3, EndLine: 9},
		{Package: "q", File: "q/q.go", StartLine: 1, EndLine: 7},
	}}}
	details := map[string]metrics.Details{"p": {
		UntestedExports:  []string{"Parse"},
		UntestedExcluded: []string{"Legacy"},
		DupLocations:     []string{"p.go:3-9", "q.go:1-7"},
		CrossBlocks:      cross,
	}}
	tests := []struct {
		name string
		ext  metrics.Extractor
		want score.Names
	}{
		{name: "detailer", ext: metricstest.NewFake("fake", root, pkgs, metricstest.WithDetails(details)),
			want: score.Names{UntestedExports: []string{"Parse"}, DupLocations: []string{"p.go:3-9", "q.go:1-7"}, CrossBlocks: cross}},
		{name: "counts only", ext: metricstest.NewFake("fake", root, pkgs)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mod := &metrics.ModuleContext{Root: root}
			got, err := suggestionNames(t.Context(), tt.ext, mod, "p")
			if err != nil {
				t.Fatalf("suggestionNames: %v", err)
			}
			if !slices.Equal(got.UntestedExports, tt.want.UntestedExports) ||
				!slices.Equal(got.DupLocations, tt.want.DupLocations) ||
				!reflect.DeepEqual(got.CrossBlocks, tt.want.CrossBlocks) {
				t.Errorf("suggestionNames = %+v, want %+v", got, tt.want)
			}
		})
	}

	t.Run("unknown package", func(t *testing.T) {
		t.Parallel()

		ext := metricstest.NewFake("fake", root, pkgs, metricstest.WithDetails(details))
		if _, err := suggestionNames(t.Context(), ext, &metrics.ModuleContext{Root: root}, "absent"); err == nil {
			t.Error("suggestionNames on an unknown package returned no error")
		}
	})
}

func TestModulePathRel(t *testing.T) {
	t.Parallel()

	tests := []struct{ importPath, want string }{
		{"example.com/m", "."},
		{"example.com/m/a", "a"},
		{"example.com/m/a/b", "a/b"},
	}
	for _, tt := range tests {
		if got := modulePathRel("example.com/m", tt.importPath); got != tt.want {
			t.Errorf("modulePathRel(%q) = %q, want %q", tt.importPath, got, tt.want)
		}
	}
}

// TestLoadTargetWarnsUnknownLanguage checks that the registry, not the
// config package, judges language ids: an override for an id no extractor
// reports is warned once, and overrides for shipped languages are not.
func TestLoadTargetWarnsUnknownLanguage(t *testing.T) {
	t.Parallel()

	const section = "\nlanguages:\n  rust:\n    rebuild:\n      cocomo_a: 3\n  go: {}\n  typescript: {}\n"
	path := filepath.Join(t.TempDir(), "astimate.yaml")
	if err := os.WriteFile(path, append(config.Default(), section...), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	warns, err := LanguageWarnings(cfg)
	if err != nil {
		t.Fatalf("LanguageWarnings: %v", err)
	}
	want := []string{"languages.rust: unknown language; known: go, typescript"}
	if !slices.Equal(warns, want) {
		t.Errorf("LanguageWarnings = %q, want %q", warns, want)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	if _, err := LoadTarget(fixtureDir, TargetOptions{ConfigPath: path, Tokenizer: TokenizerEst, Logger: logger}); err != nil {
		t.Fatalf("LoadTarget: %v", err)
	}
	var lines []string
	for line := range strings.Lines(buf.String()) {
		if strings.Contains(line, "level=WARN") && strings.Contains(line, "unknown language") {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "languages.rust") {
		t.Errorf("unknown-language warnings = %q, want one naming languages.rust", lines)
	}
	for _, id := range []string{"languages.go", "languages.typescript"} {
		if strings.Contains(buf.String(), id) {
			t.Errorf("log names %s, want no warning for a shipped language; log:\n%s", id, &buf)
		}
	}
}
