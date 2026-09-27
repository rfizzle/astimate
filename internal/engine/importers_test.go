package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// fakeImporters is a metrics.ImporterLister over a fixed reverse import
// graph, keyed by import path, that records the packages it was asked
// about.
type fakeImporters struct {
	graph map[string][]string
	asked []string
}

func (f *fakeImporters) Importers(_ context.Context, _ *metrics.ModuleContext, pkg string) ([]string, error) {
	f.asked = append(f.asked, pkg)
	from, ok := f.graph[pkg]
	if !ok {
		return nil, fmt.Errorf("importers of %s: %w", pkg, metrics.ErrUnknownPackage)
	}
	return from, nil
}

func TestSelectImporters(t *testing.T) {
	t.Parallel()

	const modPath = "example.com/m"
	head := []string{modPath, modPath + "/a", modPath + "/b", modPath + "/hub", modPath + "/lib"}
	graph := map[string][]string{
		modPath:            nil,
		modPath + "/a":     nil,
		modPath + "/b":     nil,
		modPath + "/hub":   {modPath + "/a", modPath + "/b"},
		modPath + "/lib":   {modPath},
		modPath + "/other": {modPath + "/a"},
	}
	tests := []struct {
		name      string
		changed   []string // module-relative directories already selected
		contract  []string
		want      []string // selected import paths afterwards, sorted
		wantAsked []string
		wantLogs  []string // "package importer_of" pairs logged
	}{
		{
			name:    "no contract change",
			changed: []string{"hub"},
			want:    []string{modPath + "/hub"},
		},
		{
			name:      "declaration file selects the importers",
			changed:   []string{"hub"},
			contract:  []string{"hub"},
			want:      []string{modPath + "/a", modPath + "/b", modPath + "/hub"},
			wantAsked: []string{modPath + "/hub"},
			wantLogs:  []string{"a hub", "b hub"},
		},
		{
			name:      "an importer already selected is not logged",
			changed:   []string{"a", "hub"},
			contract:  []string{"hub"},
			want:      []string{modPath + "/a", modPath + "/b", modPath + "/hub"},
			wantAsked: []string{modPath + "/hub"},
			wantLogs:  []string{"b hub"},
		},
		{
			name:      "the module root is named by its directory",
			changed:   []string{"lib"},
			contract:  []string{"lib"},
			want:      []string{modPath, modPath + "/lib"},
			wantAsked: []string{modPath + "/lib"},
			wantLogs:  []string{". lib"},
		},
		{
			name:     "a package the extractor does not list is skipped",
			changed:  []string{"other"},
			contract: []string{"other"},
			want:     []string{modPath + "/other"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			il := &fakeImporters{graph: graph}
			changed := make(map[string]bool)
			for _, dir := range tt.changed {
				changed[importPathOf(modPath, dir)] = true
			}
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			mod := &metrics.ModuleContext{Root: "/mod", ModulePath: modPath}
			if err := selectImporters(t.Context(), il, mod, modPath, head, tt.contract, changed, logger); err != nil {
				t.Fatalf("selectImporters: %v", err)
			}
			if got := slices.Sorted(maps.Keys(changed)); !slices.Equal(got, tt.want) {
				t.Errorf("selected %q, want %q", got, tt.want)
			}
			if !slices.Equal(il.asked, tt.wantAsked) {
				t.Errorf("asked for the importers of %q, want %q", il.asked, tt.wantAsked)
			}
			var got []string
			for line := range strings.Lines(logs.String()) {
				if !strings.Contains(line, "selected as an importer") {
					continue
				}
				var pkg, of string
				for f := range strings.FieldsSeq(line) {
					if v, ok := strings.CutPrefix(f, "package="); ok {
						pkg = v
					}
					if v, ok := strings.CutPrefix(f, "importer_of="); ok {
						of = v
					}
				}
				got = append(got, pkg+" "+of)
			}
			if !slices.Equal(got, tt.wantLogs) {
				t.Errorf("logged %q, want %q; logs:\n%s", got, tt.wantLogs, logs.String())
			}
		})
	}
}

func TestSelectImportersError(t *testing.T) {
	t.Parallel()

	il := &fakeImporters{graph: map[string][]string{}}
	head := []string{"example.com/m/hub"}
	err := selectImporters(t.Context(), il, &metrics.ModuleContext{Root: "/mod"}, "example.com/m", head,
		[]string{"hub"}, map[string]bool{}, slog.New(slog.DiscardHandler))
	if !errors.Is(err, metrics.ErrUnknownPackage) || !strings.Contains(err.Error(), "importers of hub") {
		t.Errorf("selectImporters error = %v, want one naming hub and wrapping ErrUnknownPackage", err)
	}
}
