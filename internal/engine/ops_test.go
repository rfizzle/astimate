package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/score"
)

// errExtract is the failure failingExtractor injects.
var errExtract = errors.New("injected extract failure")

// failingExtractor wraps an extractor and fails Extract for one package.
type failingExtractor struct {
	metrics.Extractor
	fail string
}

func (f *failingExtractor) Extract(ctx context.Context, mod *metrics.ModuleContext, pkg string) (metrics.RawMetrics, error) {
	if pkg == f.fail {
		return metrics.RawMetrics{}, errExtract
	}
	return f.Extractor.Extract(ctx, mod, pkg)
}

// rankParams are the SPEC.md 7.2 and 7.3 default rebuild parameters.
func rankParams() score.RebuildParams {
	return score.RebuildParams{
		ContextBudget:           25000,
		TokensPerExport:         40,
		TokensPerUntestedExport: 800,
		TokensPerHiddenState:    400,
		SuperlinearExponent:     1.3,
		CocomoA:                 2.4,
		CocomoB:                 1.05,
		DaysPerMonth:            19,
		Tiers:                   score.Tiers{OnePassMax: 1, FewPassesMax: 3},
	}
}

// fakeTarget returns a Target over a fake module whose Extract fails for
// the import path fail.
func fakeTarget(fail string) *Target {
	const root, modPath = "/mod", "example.com/m"
	pkgs := map[string]metrics.RawMetrics{
		modPath:              {TokensEst: 100},
		modPath + "/big":     {TokensEst: 40000, FanIn: 2},
		modPath + "/broken":  {TokensEst: 900},
		modPath + "/x/small": {TokensEst: 10},
	}
	return &Target{
		Mod: &metrics.ModuleContext{Root: root, ModulePath: modPath},
		Ext: &failingExtractor{Extractor: metricstest.NewFake("go", root, pkgs), fail: fail},
		Cfg: &config.Config{Rebuild: rankParams()},
	}
}

func rowPaths(rows []report.Row) []string {
	out := make([]string, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].Path)
	}
	return out
}

func TestRank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fail       string
		top        int
		wantPaths  []string
		wantFailed []string
	}{
		{name: "all succeed", wantPaths: []string{"big", ".", "broken", "x/small"}},
		{name: "one fails", fail: "example.com/m/broken",
			wantPaths: []string{"big", ".", "x/small"}, wantFailed: []string{"broken"}},
		{name: "top", top: 2, wantPaths: []string{"big", "."}},
		{name: "top beyond rows", top: 9, wantPaths: []string{"big", ".", "broken", "x/small"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rows, failed, err := Rank(t.Context(), fakeTarget(tt.fail), RankOptions{Sort: report.SortPasses, Top: tt.top})
			if err != nil {
				t.Fatalf("Rank: %v", err)
			}
			if got := rowPaths(rows); !slices.Equal(got, tt.wantPaths) {
				t.Errorf("rows = %q, want %q", got, tt.wantPaths)
			}
			var gotFailed []string
			for _, err := range failed {
				var pe *PackageError
				if !errors.As(err, &pe) || !errors.Is(err, errExtract) {
					t.Fatalf("failure %v is not a *PackageError wrapping the extract error", err)
				}
				gotFailed = append(gotFailed, pe.Path)
			}
			if !slices.Equal(gotFailed, tt.wantFailed) {
				t.Errorf("failed = %q, want %q", gotFailed, tt.wantFailed)
			}
		})
	}

	t.Run("unknown sort key", func(t *testing.T) {
		t.Parallel()

		if _, _, err := Rank(t.Context(), fakeTarget(""), RankOptions{Sort: "nope"}); !errors.Is(err, report.ErrUnknownSortKey) {
			t.Errorf("Rank error = %v, want report.ErrUnknownSortKey", err)
		}
	})
	t.Run("list failure", func(t *testing.T) {
		t.Parallel()

		tg := fakeTarget("")
		tg.Mod.Root = "/elsewhere"
		if rows, _, err := Rank(t.Context(), tg, RankOptions{Sort: report.SortPasses}); err == nil || rows != nil {
			t.Errorf("Rank = (%v, %v), want no rows and an error", rows, err)
		}
	})
}

func TestLoadTargetTokenizer(t *testing.T) {
	t.Parallel()

	if _, err := LoadTarget(fixtureDir, TargetOptions{Tokenizer: "bogus"}); !errors.Is(err, ErrUnknownTokenizer) {
		t.Errorf("LoadTarget error = %v, want ErrUnknownTokenizer", err)
	}
	cfg := &config.Config{Version: "given"}
	tg, err := LoadTarget(fixtureDir, TargetOptions{Config: cfg})
	if err != nil {
		t.Fatalf("LoadTarget: %v", err)
	}
	if tg.Cfg != cfg || tg.ConfigSource != "" {
		t.Errorf("LoadTarget with a Config = (%p, %q), want (%p, \"\")", tg.Cfg, tg.ConfigSource, cfg)
	}
	if _, err := LoadTarget(filepath.Join(fixtureDir, "go.mod"), TargetOptions{}); !errors.Is(err, ErrNotDir) {
		t.Errorf("LoadTarget on a file: error = %v, want ErrNotDir", err)
	}
}

func TestAssess(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	tg, err := LoadTarget(filepath.Join(fixtureDir, "hub"), TargetOptions{Version: "v9"})
	if err != nil {
		t.Fatalf("LoadTarget: %v", err)
	}
	r, err := Assess(t.Context(), tg, AssessOptions{})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if r.PackagePath != "hub" || r.ModulePath != "example.com/fixture" || r.AstimateVersion != "v9" {
		t.Errorf("report = (%q, %q, %q), want (hub, example.com/fixture, v9)",
			r.PackagePath, r.ModulePath, r.AstimateVersion)
	}
}

func TestCheckRejectsBothBaselines(t *testing.T) {
	t.Parallel()

	_, _, err := Check(t.Context(), fakeTarget(""), CheckOptions{Base: "master", BaselineFile: "b.json"})
	if !errors.Is(err, ErrBaseAndBaselineFile) {
		t.Errorf("Check error = %v, want ErrBaseAndBaselineFile", err)
	}
}

func TestWriteBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	root := t.TempDir()
	files := map[string]string{
		"go.mod":   "module example.com/w\n\ngo 1.22\n",
		"w.go":     "package w\n\n// F is exported.\nfunc F() int { return 1 }\n",
		"sub/s.go": "package sub\n\n// G is exported.\nfunc G() int { return 2 }\n",
	}
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tg, err := LoadTarget(root, TargetOptions{})
	if err != nil {
		t.Fatalf("LoadTarget: %v", err)
	}
	path, n, err := WriteBaseline(t.Context(), tg, "")
	if err != nil {
		t.Fatalf("WriteBaseline: %v", err)
	}
	if want := filepath.Join(tg.Mod.Root, DefaultBaselinePath); path != want || n != 2 {
		t.Errorf("WriteBaseline = (%q, %d), want (%q, 2)", path, n, want)
	}
	b, err := baseline.FromFile(path)
	if err != nil {
		t.Fatalf("reading the baseline: %v", err)
	}
	if _, ok := b.Metrics("example.com/w/sub"); !ok {
		t.Error("baseline has no metrics for example.com/w/sub")
	}
	// The count excludes the module row, which the file holds beside them.
	if m, ok := b.Metrics(metrics.ModuleRowID); !ok || m.DupBlocksCrossPkg == nil || *m.DupBlocksCrossPkg != 0 {
		t.Errorf("baseline module row = %+v (present %v), want dup_blocks_cross_pkg 0", m, ok)
	}
	if b.Tokenizer() != TokenizerEst {
		t.Errorf("baseline records tokenizer %q, want the target's %q", b.Tokenizer(), TokenizerEst)
	}
}

// TestImportBoundary checks that engine and the packages under its
// internal directory sit below their callers: none of them, nor anything
// they depend on, imports the CLI or the MCP server.
func TestImportBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	t.Parallel()

	out, err := exec.CommandContext(t.Context(), "go", "list", "-deps", "-f", "{{.ImportPath}}", "./...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for dep := range strings.FieldsSeq(string(out)) {
		if strings.Contains(dep, "/astimate/cmd/") || strings.HasSuffix(dep, "/internal/mcpserver") {
			t.Errorf("engine depends on %s", dep)
		}
	}
	if !strings.Contains(string(out), "/internal/engine/internal/judge\n") {
		t.Error("go list ./... does not list internal/judge; the check would pass vacuously")
	}
}

// TestGoExtractorBoundary checks that the Go extractor and every package
// under it sit below the gate: none depends on engine, report, gate, score
// or config, which compose extractors and never the reverse.
func TestGoExtractorBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	t.Parallel()

	out, err := exec.CommandContext(t.Context(), "go", "list", "-deps", "-f", "{{.ImportPath}}", "../lang/golang/...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	forbidden := []string{"/internal/engine", "/internal/report", "/internal/gate", "/internal/score", "/internal/config"}
	for dep := range strings.FieldsSeq(string(out)) {
		for _, f := range forbidden {
			if strings.HasSuffix(dep, f) {
				t.Errorf("internal/lang/golang/... depends on %s", dep)
			}
		}
	}
}
