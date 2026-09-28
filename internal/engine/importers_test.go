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

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/engine/internal/judge"
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
				changed[judge.ImportPath(modPath, dir)] = true
			}
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			mod := &metrics.ModuleContext{Root: "/mod", ModulePath: modPath}
			if err := selectImporters(t.Context(), il, mod, modPath, head, baseline.Change{Contract: tt.contract}, nil, changed, logger); err != nil {
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
		baseline.Change{Contract: []string{"hub"}}, nil, map[string]bool{}, slog.New(slog.DiscardHandler))
	if !errors.Is(err, metrics.ErrUnknownPackage) || !strings.Contains(err.Error(), "importers of hub") {
		t.Errorf("selectImporters error = %v, want one naming hub and wrapping ErrUnknownPackage", err)
	}
}

// fakeGraph is an importerGraph over a fixed reverse import graph, or one
// that recorded none when known is false.
type fakeGraph struct {
	graph map[string][]string
	known bool
}

func (g fakeGraph) Importers(pkg string) ([]string, bool) { return g.graph[pkg], g.known }

func TestSelectImportersAtBaseline(t *testing.T) {
	t.Parallel()

	const modPath = "example.com/m"
	head := []string{modPath + "/a", modPath + "/b", modPath + "/c", modPath + "/hub"}
	headGraph := map[string][]string{
		modPath + "/a":   nil,
		modPath + "/b":   nil,
		modPath + "/c":   nil,
		modPath + "/hub": {modPath + "/b"},
	}
	baseGraph := map[string][]string{
		modPath + "/hub":  {modPath + "/a", modPath + "/b"},
		modPath + "/gone": {modPath + "/c", modPath + "/old"},
	}
	tests := []struct {
		name     string
		changed  []string
		change   baseline.Change
		base     importerGraph
		want     []string
		wantLogs []string // "graph package importer_of" triples logged
		wantNote bool     // the no-graph line is logged, once
	}{
		{
			name:     "removed declaration file selects its former importer",
			changed:  []string{"hub"},
			change:   baseline.Change{Contract: []string{"hub"}, RemovedContract: []string{"hub"}},
			base:     fakeGraph{graph: baseGraph, known: true},
			want:     []string{modPath + "/a", modPath + "/b", modPath + "/hub"},
			wantLogs: []string{"head b hub", "baseline a hub"},
		},
		{
			name:     "a modified declaration file takes the union of head and baseline",
			changed:  []string{"hub"},
			change:   baseline.Change{Contract: []string{"hub"}},
			base:     fakeGraph{graph: baseGraph, known: true},
			want:     []string{modPath + "/a", modPath + "/b", modPath + "/hub"},
			wantLogs: []string{"head b hub", "baseline a hub"},
		},
		{
			name:     "a removed package's former importers still at head are selected",
			change:   baseline.Change{RemovedContract: []string{"gone"}},
			base:     fakeGraph{graph: baseGraph, known: true},
			want:     []string{modPath + "/c"},
			wantLogs: []string{"baseline c gone"},
		},
		{
			name:     "a baseline without a graph is noted once",
			changed:  []string{"hub"},
			change:   baseline.Change{Contract: []string{"hub"}, RemovedContract: []string{"gone", "hub"}},
			base:     fakeGraph{},
			want:     []string{modPath + "/b", modPath + "/hub"},
			wantLogs: []string{"head b hub"},
			wantNote: true,
		},
		{
			name:     "no baseline at all is noted",
			change:   baseline.Change{RemovedContract: []string{"hub"}},
			want:     []string{},
			wantNote: true,
		},
		{
			name:     "a modified declaration file needs no note without a graph",
			changed:  []string{"hub"},
			change:   baseline.Change{Contract: []string{"hub"}},
			base:     fakeGraph{},
			want:     []string{modPath + "/b", modPath + "/hub"},
			wantLogs: []string{"head b hub"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			changed := make(map[string]bool)
			for _, dir := range tt.changed {
				changed[judge.ImportPath(modPath, dir)] = true
			}
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			mod := &metrics.ModuleContext{Root: "/mod", ModulePath: modPath}
			il := &fakeImporters{graph: headGraph}
			if err := selectImporters(t.Context(), il, mod, modPath, head, tt.change, tt.base, changed, logger); err != nil {
				t.Fatalf("selectImporters: %v", err)
			}
			if got := slices.Sorted(maps.Keys(changed)); !slices.Equal(got, tt.want) {
				t.Errorf("selected %q, want %q", got, tt.want)
			}
			var got []string
			notes := 0
			for line := range strings.Lines(logs.String()) {
				if strings.Contains(line, "records no import graph") {
					notes++
					continue
				}
				graph := "head"
				if strings.Contains(line, "importer at baseline") {
					graph = "baseline"
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
				got = append(got, graph+" "+pkg+" "+of)
			}
			if !slices.Equal(got, tt.wantLogs) {
				t.Errorf("logged %q, want %q; logs:\n%s", got, tt.wantLogs, logs.String())
			}
			wantNotes := 0
			if tt.wantNote {
				wantNotes = 1
			}
			if notes != wantNotes {
				t.Errorf("logged the no-graph note %d times, want %d; logs:\n%s", notes, wantNotes, logs.String())
			}
		})
	}
}
