package engine

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
)

// writeFakeBaseline writes a baseline file for fakeTarget's module in a
// temporary directory and returns its path.
func writeFakeBaseline(t *testing.T) string {
	t.Helper()
	return writeFakeBaselineTokenizer(t, TokenizerEst)
}

// writeFakeBaselineTokenizer is writeFakeBaseline recording tokenizer.
func writeFakeBaselineTokenizer(t *testing.T, tokenizer string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "baseline.json")
	pkgs := map[string]metrics.RawMetrics{"example.com/m/big": {TokensEst: 39000}}
	if err := baseline.Write(path, "", "example.com/m", tokenizer, pkgs); err != nil {
		t.Fatalf("writing baseline: %v", err)
	}
	return path
}

func TestCheckPackagesOverride(t *testing.T) {
	t.Parallel()

	tg := fakeTarget("")
	c, failed, err := Check(t.Context(), tg, CheckOptions{
		BaselineFile: writeFakeBaseline(t),
		Packages:     []string{"example.com/m/big"},
	})
	if err != nil || len(failed) != 0 {
		t.Fatalf("Check = (%v, %v), want no error", failed, err)
	}
	if len(c.Packages) != 1 || c.Packages[0].Report.PackagePath != "big" {
		t.Fatalf("checked %+v, want only big", c.Packages)
	}
	if c.Packages[0].Report.Baseline == nil {
		t.Error("big has no baseline block, want the file's metrics")
	}
	if c.Deleted != nil {
		t.Errorf("Deleted = %v, want none with an explicit package list", c.Deleted)
	}
}

func TestCheckTokenizerMismatch(t *testing.T) {
	t.Parallel()

	const warning = "baseline tokenizer o200k differs from check tokenizer est; token counts are not comparable"
	tests := []struct {
		name      string
		file      string // tokenizer recorded in the baseline file
		check     string // the target's tokenizer; empty means est
		wantWarns int
	}{
		{name: "mismatch", file: TokenizerO200k, check: TokenizerEst, wantWarns: 1},
		{name: "mismatch with default tokenizer", file: TokenizerO200k, wantWarns: 1},
		{name: "equal", file: TokenizerEst, check: TokenizerEst},
		{name: "equal o200k", file: TokenizerO200k, check: TokenizerO200k},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer
			tg := fakeTarget("")
			tg.Tokenizer = tt.check
			tg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
			c, failed, err := Check(t.Context(), tg, CheckOptions{
				BaselineFile: writeFakeBaselineTokenizer(t, tt.file),
				Packages:     []string{"example.com/m/big"},
			})
			if err != nil || len(failed) != 0 {
				t.Fatalf("Check = (%v, %v), want no error despite the tokenizers", failed, err)
			}
			// The report carries what the log says, for JSON, hook and
			// GitHub consumers that never see stderr.
			b := c.Packages[0].Report.Baseline
			if b == nil || b.Tokenizer != tt.file || b.TokensComparable != (tt.wantWarns == 0) {
				t.Errorf("baseline block = %+v, want tokenizer %s, comparable %v", b, tt.file, tt.wantWarns == 0)
			}
			if got := strings.Count(logs.String(), "differs from check tokenizer"); got != tt.wantWarns {
				t.Errorf("logged %d tokenizer warnings, want %d; logs:\n%s", got, tt.wantWarns, logs.String())
			}
			if tt.wantWarns > 0 && !strings.Contains(logs.String(), "level=WARN msg=\""+warning+"\"") {
				t.Errorf("logs = %q, want a warning %q", logs.String(), warning)
			}
		})
	}
}

// TestCheckWithoutSourceClassifier checks that an extractor that cannot
// say which files change a package gets every package checked, with one
// warning saying why.
func TestCheckWithoutSourceClassifier(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	tg := fakeTarget("")
	tg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	if _, ok := tg.Ext.(metrics.SourceClassifier); ok {
		t.Fatal("the fake extractor implements metrics.SourceClassifier")
	}
	c, failed, err := Check(t.Context(), tg, CheckOptions{BaselineFile: writeFakeBaseline(t)})
	if err != nil || len(failed) != 0 {
		t.Fatalf("Check = (%v, %v), want no error", failed, err)
	}
	if len(c.Packages) != 4 {
		t.Errorf("checked %d packages, want all 4", len(c.Packages))
	}
	if n := strings.Count(logs.String(), "level=WARN"); n != 1 {
		t.Errorf("logged %d warnings, want 1:\n%s", n, logs.String())
	}
	if want := "cannot tell which files changed a package; checking every package"; !strings.Contains(logs.String(), want) {
		t.Errorf("logs = %q, want %q", logs.String(), want)
	}
}

func TestCheckNoBaseline(t *testing.T) {
	t.Parallel()

	// A module outside any git repository has no default ref and no
	// baseline file.
	root := t.TempDir()
	tg := &Target{
		Mod: &metrics.ModuleContext{Root: root, ModulePath: "example.com/m"},
		Ext: metricstest.NewFake("go", root, map[string]metrics.RawMetrics{"example.com/m": {}}),
		Cfg: &config.Config{Rebuild: rankParams()},
	}
	_, _, err := Check(t.Context(), tg, CheckOptions{Packages: []string{"example.com/m"}})
	if !errors.Is(err, ErrNoBaseline) {
		t.Fatalf("Check error = %v, want ErrNoBaseline", err)
	}
	if strings.Contains(err.Error(), "--base") {
		t.Errorf("Check error = %q, want no caller-specific hint", err)
	}
}

func TestBaselineCacheFile(t *testing.T) {
	t.Parallel()

	path := writeFakeBaseline(t)
	cache := NewBaselineCache()
	first, err := fileBaseline(path, cache, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	again, err := fileBaseline(path, cache, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if again != first || cache.Loads() != 1 {
		t.Errorf("second read: same = %v, loads = %d, want the cached baseline and 1 load", again == first, cache.Loads())
	}

	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	edited, err := fileBaseline(path, cache, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if edited == first || cache.Loads() != 2 {
		t.Errorf("after a touch: same = %v, loads = %d, want a fresh read and 2 loads", edited == first, cache.Loads())
	}

	var none *BaselineCache
	if _, err := fileBaseline(path, none, slog.New(slog.DiscardHandler)); err != nil {
		t.Errorf("reading without a cache: %v", err)
	}
}
