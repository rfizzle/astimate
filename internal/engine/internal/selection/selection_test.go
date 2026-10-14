package selection

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
)

// classifier wraps an extractor with a metrics.SourceClassifier that
// classifies nothing; the tests below never reach git.
type classifier struct{ metrics.Extractor }

func (classifier) ClassifyFile(string) metrics.SourceFile { return metrics.SourceFile{} }
func (classifier) IsModuleMarker(string) bool             { return false }

// refless writes a baseline file recording no git ref and reads it back.
func refless(t *testing.T) baseline.Baseline {
	t.Helper()
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := baseline.WriteContents(path, baseline.Contents{ModulePath: "example.com/m"}); err != nil {
		t.Fatal(err)
	}
	b, err := baseline.FromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestPackagesEveryPackage checks the selections that need no git: each
// returns every head package and says why when it is a fallback.
func TestPackagesEveryPackage(t *testing.T) {
	t.Parallel()

	const root, modPath = "/mod", "example.com/m"
	head := []string{modPath, modPath + "/a"}
	fake := metricstest.NewFake("go", root, nil)
	tests := []struct {
		name    string
		ext     metrics.Extractor
		all     bool
		file    bool
		wantLog string
	}{
		{name: "all", ext: classifier{fake}, all: true},
		{name: "no source classifier", ext: fake, wantLog: "cannot tell which files changed"},
		{name: "baseline file without a ref", ext: classifier{fake}, file: true, wantLog: "records no git ref"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			r := Request{
				Ext: tt.ext, Mod: &metrics.ModuleContext{Root: root, ModulePath: modPath}, Packages: head,
				Source: Source{Ref: "main"}, All: tt.all, Logger: slog.New(slog.NewTextHandler(&logs, nil)),
			}
			if tt.file {
				r.Source = Source{File: refless(t)}
			}
			selected, deleted, err := Packages(t.Context(), r)
			if err != nil {
				t.Fatalf("Packages: %v", err)
			}
			if !slices.Equal(selected, head) || deleted != nil {
				t.Errorf("Packages = %v, %v; want %v, nil", selected, deleted, head)
			}
			if !strings.Contains(logs.String(), tt.wantLog) || (tt.wantLog == "" && logs.Len() > 0) {
				t.Errorf("logs = %q, want %q", logs.String(), tt.wantLog)
			}
		})
	}
}

// TestSeedWithoutMergeBase checks that Seed returns the baseline
// unchanged when no merge-base is known: the extractor cannot classify
// sources, or the baseline file records no ref.
func TestSeedWithoutMergeBase(t *testing.T) {
	t.Parallel()

	fake := metricstest.NewFake("go", "/mod", nil)
	base := refless(t)
	for _, ext := range []metrics.Extractor{fake, classifier{fake}} {
		r := Request{Ext: ext, Mod: &metrics.ModuleContext{Root: "/mod"}, Source: Source{File: base}}
		got, err := Seed(t.Context(), r, base)
		if err != nil || got != base {
			t.Errorf("Seed with %T = %v, %v; want the baseline unchanged", ext, got, err)
		}
	}
}
