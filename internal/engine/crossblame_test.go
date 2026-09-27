package engine

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// block returns a cross-package block with one occurrence in each of pkgs,
// packages of example.com/m, in the given order.
func block(pkgs ...string) metrics.CrossBlock {
	b := metrics.CrossBlock{Occurrences: make([]metrics.Occurrence, 0, len(pkgs))}
	for i, p := range pkgs {
		b.Occurrences = append(b.Occurrences, metrics.Occurrence{
			Package: "example.com/m/" + p, File: p + "/" + p + ".go", StartLine: 10 * (i + 1), EndLine: 10*(i+1) + 5,
		})
	}
	return b
}

// blockNames renders blocks as their package sets, "a-b", for comparison.
func blockNames(blocks []metrics.CrossBlock) []string {
	out := make([]string, 0, len(blocks))
	for i := range blocks {
		pkgs := blockPackages(&blocks[i])
		for j, p := range pkgs {
			pkgs[j] = modulePathRel("example.com/m", p)
		}
		out = append(out, strings.Join(pkgs, "-"))
	}
	return out
}

func TestBlameCross(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		head, base []metrics.CrossBlock
		known      bool
		named      string
		want       []string
		wantGated  int
	}{
		{
			name:  "copy between two other packages",
			head:  []metrics.CrossBlock{block("a", "b"), block("hidden", "hub")},
			base:  []metrics.CrossBlock{block("a", "b")},
			known: true, named: "trivial", wantGated: 1,
		},
		{
			name:  "new copy touching the named package",
			head:  []metrics.CrossBlock{block("a", "b"), block("hidden", "hub")},
			base:  []metrics.CrossBlock{block("a", "b")},
			known: true, named: "hub", want: []string{"hidden-hub"}, wantGated: 2,
		},
		{
			name:  "old copy touching the named package, new one elsewhere",
			head:  []metrics.CrossBlock{block("a", "hub"), block("b", "c")},
			base:  []metrics.CrossBlock{block("a", "hub")},
			known: true, named: "hub", wantGated: 1,
		},
		{
			name:  "second copy between the same packages is new",
			head:  []metrics.CrossBlock{block("a", "hub"), block("a", "hub")},
			base:  []metrics.CrossBlock{block("a", "hub")},
			known: true, named: "hub", want: []string{"a-hub"}, wantGated: 2,
		},
		{
			name:  "copy removed elsewhere does not hide a new one",
			head:  []metrics.CrossBlock{block("a", "hub")},
			base:  []metrics.CrossBlock{block("b", "c"), block("c", "d")},
			known: true, named: "hub", want: []string{"a-hub"}, wantGated: 3,
		},
		{
			name:  "no block identities, no block touches the named package",
			head:  []metrics.CrossBlock{block("a", "b"), block("hidden", "hub")},
			named: "trivial", wantGated: 1,
		},
		{
			name:  "no block identities, an old block touches the named package",
			head:  []metrics.CrossBlock{block("a", "hub"), block("b", "c")},
			named: "hub", want: []string{"a-hub"}, wantGated: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			named := map[string]bool{"example.com/m/" + tt.named: true}
			b := blameCross(tt.head, tt.base, tt.known, named)
			if got := blockNames(b.blocks); !slices.Equal(got, tt.want) {
				t.Errorf("blamed %v, want %v", got, tt.want)
			}
			// Without identities the baseline file records only a count:
			// the head count before the last copy here.
			baseCount := len(tt.base)
			if !tt.known {
				baseCount = len(tt.head) - 1
			}
			if got := b.gatedCross(len(tt.head), baseCount); got != tt.wantGated {
				t.Errorf("gatedCross = %d, want %d", got, tt.wantGated)
			}
		})
	}
}

func TestCrossBlameSuggestion(t *testing.T) {
	t.Parallel()

	b := crossBlame{blocks: []metrics.CrossBlock{block("hidden", "hub"), block("a", "b", "hub")}, known: true}
	got := b.suggestion(metrics.RawMetrics{}, "example.com/m")
	const want = "2 duplicate blocks are shared with other packages; extract each into one package, " +
		"starting with the block in hidden/hidden.go:10-15 and hub/hub.go:20-25. " +
		"New blocks touching the checked package are shared by hidden and hub; a, b and hub."
	if got != want {
		t.Errorf("suggestion =\n%q\nwant\n%q", got, want)
	}
	if s := (crossBlame{known: true}).suggestion(metrics.RawMetrics{}, "example.com/m"); s != "" {
		t.Errorf("suggestion with no blamed block = %q, want empty", s)
	}
}

// detailedModuleExtractor is a moduleExtractor that names its module row's
// cross-package blocks (metrics.ModuleDetailer).
type detailedModuleExtractor struct {
	*moduleExtractor
	blocks []metrics.CrossBlock
}

func (e *detailedModuleExtractor) ModuleDetails(context.Context, *metrics.ModuleContext) (metrics.Details, error) {
	return metrics.Details{CrossBlocks: e.blocks}, nil
}

// TestCheckModuleRowNamedPackageFilter checks a named-package check against
// a baseline file, which records no block identities: a new cross-package
// block fails the check of a package it touches, naming the packages that
// share it, and not the check of one it does not touch, whose module row
// still reports the full count. A check of every package is unchanged.
func TestCheckModuleRowNamedPackageFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		blocks     []metrics.CrossBlock
		packages   []string
		wantPassed bool
		wantShared string
	}{
		{name: "copy between other packages", blocks: []metrics.CrossBlock{block("b", "c")},
			packages: []string{"example.com/m/a"}, wantPassed: true},
		{name: "copy touching the checked package", blocks: []metrics.CrossBlock{block("a", "c")},
			packages: []string{"example.com/m/a"}, wantShared: "shared by a and c."},
		{name: "every package", blocks: []metrics.CrossBlock{block("b", "c")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			zero := 0
			tg := moduleTarget(len(tt.blocks), nil)
			tg.Ext = &detailedModuleExtractor{moduleExtractor: tg.Ext.(*moduleExtractor), blocks: tt.blocks}
			c, failed, err := Check(t.Context(), tg, CheckOptions{
				BaselineFile: writeModuleBaseline(t, &zero),
				Packages:     tt.packages,
				All:          len(tt.packages) == 0,
			})
			if err != nil || len(failed) != 0 {
				t.Fatalf("Check = (%v, %v), want no error", failed, err)
			}
			r := c.Module.Report
			if n := r.Metrics.DupBlocksCrossPkg; n == nil || *n != len(tt.blocks) {
				t.Errorf("module row dup_blocks_cross_pkg = %v, want the full count %d", n, len(tt.blocks))
			}
			if c.Failed() == tt.wantPassed || (len(r.Violations) == 0) != tt.wantPassed {
				t.Fatalf("check failed = %v with module violations %+v, want passed %v", c.Failed(), r.Violations, tt.wantPassed)
			}
			if tt.wantShared != "" && !strings.HasSuffix(r.Violations[0].Suggestion, tt.wantShared) {
				t.Errorf("suggestion = %q, want it to end %q", r.Violations[0].Suggestion, tt.wantShared)
			}
		})
	}
}
