package engine

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
)

// listingExtractor is a fake extractor that also implements
// metrics.FunctionLister from a fixed map of functions per package.
type listingExtractor struct {
	metrics.Extractor
	funcs map[string][]metrics.FunctionInfo
}

func (l *listingExtractor) Functions(_ context.Context, _ *metrics.ModuleContext, pkg string) ([]metrics.FunctionInfo, error) {
	return l.funcs[pkg], nil
}

func TestCheckChangedFunctions(t *testing.T) {
	t.Parallel()

	const root, modPath = "/mod", "example.com/m"
	pkgs := map[string]metrics.RawMetrics{modPath + "/old": {}, modPath + "/new": {}}
	parse := metrics.FunctionInfo{Name: "Parse", Fingerprint: 1, Cognitive: 5, File: "p.go", Line: 3}
	grade := metrics.FunctionInfo{Receiver: "T", Name: "grade", Fingerprint: 2, Cognitive: 40, File: "g.go", Line: 7}
	small := metrics.FunctionInfo{Name: "small", Fingerprint: 3, Cognitive: 12, File: "s.go", Line: 1}
	edited := parse
	edited.Fingerprint = 9
	edited.Cognitive = 31

	baseFuncs := map[string][]metrics.FunctionInfo{modPath + "/old": {parse}}
	tests := []struct {
		name      string
		head      map[string][]metrics.FunctionInfo // nil: the extractor lists no functions
		baseFuncs map[string][]metrics.FunctionInfo // nil: the file records none
		pkg       string
		want      *int
		violation string // the suggestion's text, when the rule should fail
		skipLog   bool
	}{
		{
			name: "added complex function fails and is named", baseFuncs: baseFuncs, pkg: modPath + "/old",
			head:      map[string][]metrics.FunctionInfo{modPath + "/old": {parse, grade}},
			want:      ptr(40),
			violation: "Changed function T.grade (g.go:7) has cognitive complexity 40",
		},
		{
			name: "unchanged functions report 0", baseFuncs: baseFuncs, pkg: modPath + "/old",
			head: map[string][]metrics.FunctionInfo{modPath + "/old": {parse}},
			want: ptr(0),
		},
		{
			name: "modified function counts", baseFuncs: baseFuncs, pkg: modPath + "/old",
			head:      map[string][]metrics.FunctionInfo{modPath + "/old": {edited, small}},
			want:      ptr(31),
			violation: "Changed function Parse (p.go:3) has cognitive complexity 31",
		},
		{
			name: "every function of a new package is changed", baseFuncs: baseFuncs, pkg: modPath + "/new",
			head: map[string][]metrics.FunctionInfo{modPath + "/new": {small}},
			want: ptr(12),
		},
		{
			name: "baseline file without functions leaves it null", pkg: modPath + "/old",
			head:    map[string][]metrics.FunctionInfo{modPath + "/old": {parse, grade}},
			skipLog: true,
		},
		{
			name: "extractor without a function lister leaves it null", baseFuncs: baseFuncs, pkg: modPath + "/old",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "baseline.json")
			err := baseline.WriteContents(path, baseline.Contents{
				ModulePath: modPath,
				Packages:   map[string]metrics.RawMetrics{modPath + "/old": {}},
				Functions:  tt.baseFuncs,
			})
			if err != nil {
				t.Fatal(err)
			}
			ext := metricstest.NewFake("go", root, pkgs)
			if tt.head != nil {
				ext = &listingExtractor{Extractor: ext, funcs: tt.head}
			}
			var logs bytes.Buffer
			tg := &Target{
				Mod:    &metrics.ModuleContext{Root: root, ModulePath: modPath},
				Ext:    ext,
				Logger: slog.New(slog.NewTextHandler(&logs, nil)),
				Cfg: &config.Config{Rebuild: rankParams(), Thresholds: []gate.Threshold{
					{Metric: "changed_func_cognitive_max", Kind: gate.Density, Max: ptr(30.0)},
				}},
			}
			c, failed, err := Check(t.Context(), tg, CheckOptions{BaselineFile: path, Packages: []string{tt.pkg}})
			if err != nil || len(failed) != 0 {
				t.Fatalf("Check = (%v, %v), want no error", failed, err)
			}
			r := c.Packages[0].Report
			got := r.Metrics.ChangedFuncCognitiveMax
			if (got == nil) != (tt.want == nil) || got != nil && *got != *tt.want {
				t.Errorf("changed_func_cognitive_max = %v, want %v", deref(got), deref(tt.want))
			}
			if tt.violation == "" {
				if len(r.Violations) != 0 {
					t.Errorf("violations = %+v, want none", r.Violations)
				}
			} else if len(r.Violations) != 1 || !strings.HasPrefix(r.Violations[0].Suggestion, tt.violation) {
				t.Errorf("violations = %+v, want one whose suggestion starts %q", r.Violations, tt.violation)
			}
			logged := strings.Contains(logs.String(), "metric=changed_func_cognitive_max")
			if logged != tt.skipLog {
				t.Errorf("logged the skipped rule = %v, want %v; logs:\n%s", logged, tt.skipLog, logs.String())
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

// deref renders a nullable metric for messages.
func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
