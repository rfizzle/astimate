package engine

import (
	"context"
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
