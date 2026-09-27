package engine

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
	"github.com/rfizzle/astimate/internal/report"
)

// moduleExtractor is a fake extractor with a module row carrying
// dup_blocks_cross_pkg = cross, or failing with err.
type moduleExtractor struct {
	metrics.Extractor
	cross int
	err   error
}

func (e *moduleExtractor) ModuleRow(context.Context, *metrics.ModuleContext) (metrics.RawMetrics, error) {
	if e.err != nil {
		return metrics.RawMetrics{}, e.err
	}
	n := e.cross
	return metrics.RawMetrics{DupBlocksCrossPkg: &n}, nil
}

// moduleTarget returns a target over two fake packages whose extractor has
// a module row, gated by the rule the design proposes for
// dup_blocks_cross_pkg: max_delta 0 with ratchet_from_zero.
func moduleTarget(cross int, err error) *Target {
	const root, modPath = "/mod", "example.com/m"
	pkgs := map[string]metrics.RawMetrics{
		modPath + "/a": {TokensEst: 100},
		modPath + "/b": {TokensEst: 200},
	}
	zero := 0.0
	return &Target{
		Mod: &metrics.ModuleContext{Root: root, ModulePath: modPath},
		Ext: &moduleExtractor{Extractor: metricstest.NewFake("go", root, pkgs), cross: cross, err: err},
		Cfg: &config.Config{Rebuild: rankParams(), Thresholds: []gate.Threshold{
			{Metric: "dup_blocks_cross_pkg", Kind: gate.Density, MaxDelta: &zero, RatchetFromZero: true},
		}},
	}
}

// writeModuleBaseline writes a baseline for moduleTarget's packages, with a
// module row carrying dup_blocks_cross_pkg = *cross unless cross is nil.
func writeModuleBaseline(t *testing.T, cross *int) string {
	t.Helper()
	pkgs := map[string]metrics.RawMetrics{
		"example.com/m/a": {TokensEst: 100},
		"example.com/m/b": {TokensEst: 200},
	}
	if cross != nil {
		pkgs[metrics.ModuleRowID] = metrics.RawMetrics{DupBlocksCrossPkg: cross}
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := baseline.Write(path, "", "example.com/m", TokenizerEst, pkgs); err != nil {
		t.Fatal(err)
	}
	return path
}

// noModuleRowNote is the log check writes when a baseline file has no
// module row.
const noModuleRowNote = "baseline file has no module row; module-wide rules skipped; run `astimate baseline write` to add it"

func TestCheckModuleRow(t *testing.T) {
	t.Parallel()

	one, two := 1, 2
	tests := []struct {
		name       string
		head       int
		base       *int
		wantPassed bool
		wantBase   bool
		wantNote   bool
	}{
		{name: "unchanged", head: 1, base: &one, wantPassed: true, wantBase: true},
		{name: "improved", head: 1, base: &two, wantPassed: true, wantBase: true},
		{name: "one more shared block", head: 2, base: &one, wantBase: true},
		{name: "no module row in the baseline, none at head", head: 0, wantPassed: true, wantNote: true},
		// A file written before the module row existed: its shared blocks
		// are not new, so the rule is skipped rather than ratcheted from
		// zero.
		{name: "no module row in the baseline, rules skipped", head: 1, wantPassed: true, wantNote: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer
			tg := moduleTarget(tt.head, nil)
			tg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
			c, failed, err := Check(t.Context(), tg, CheckOptions{
				BaselineFile: writeModuleBaseline(t, tt.base),
				All:          true,
			})
			if err != nil || len(failed) != 0 {
				t.Fatalf("Check = (%v, %v), want no error", failed, err)
			}
			if n, want := strings.Count(logs.String(), noModuleRowNote), map[bool]int{true: 1}[tt.wantNote]; n != want {
				t.Errorf("logged %d no-module-row notes, want %d; logs:\n%s", n, want, logs.String())
			}
			if c.Module == nil {
				t.Fatal("check has no module row")
			}
			r := c.Module.Report
			if r.PackagePath != metrics.ModuleRowID || r.Metrics.DupBlocksCrossPkg == nil || *r.Metrics.DupBlocksCrossPkg != tt.head {
				t.Errorf("module row = %s with dup_blocks_cross_pkg %v, want %s with %d",
					r.PackagePath, r.Metrics.DupBlocksCrossPkg, metrics.ModuleRowID, tt.head)
			}
			if r.Passed == nil || *r.Passed != tt.wantPassed || c.Failed() == tt.wantPassed {
				t.Errorf("module row passed = %v, check failed = %v, want passed %v", r.Passed, c.Failed(), tt.wantPassed)
			}
			if (r.Baseline != nil) != tt.wantBase {
				t.Errorf("module row baseline = %+v, want present %v", r.Baseline, tt.wantBase)
			}
			if !tt.wantPassed && (len(r.Violations) != 1 || r.Violations[0].Suggestion == "") {
				t.Errorf("violations = %+v, want one dup_blocks_cross_pkg finding with a suggestion", r.Violations)
			}
			if len(c.Packages) != 2 {
				t.Errorf("checked %d packages, want 2 beside the module row", len(c.Packages))
			}
		})
	}
}

// TestCheckModuleRowChosenPackages checks that a check of named packages,
// as the MCP check_package tool runs, still gates the module row, so a
// cross-package copy fails a one-package self-check.
func TestCheckModuleRowChosenPackages(t *testing.T) {
	t.Parallel()

	one := 1
	c, failed, err := Check(t.Context(), moduleTarget(2, nil), CheckOptions{
		BaselineFile: writeModuleBaseline(t, &one),
		Packages:     []string{"example.com/m/a"},
	})
	if err != nil || len(failed) != 0 {
		t.Fatalf("Check = (%v, %v), want no error", failed, err)
	}
	if len(c.Packages) != 1 || c.Packages[0].Report.PackagePath != "a" {
		t.Fatalf("checked %d packages, want only a", len(c.Packages))
	}
	if c.Module == nil {
		t.Fatal("check of a chosen package has no module row")
	}
	if v := c.Module.Report.Violations; len(v) != 1 || v[0].Metric != "dup_blocks_cross_pkg" || !c.Failed() {
		t.Errorf("module row violations = %+v, check failed = %v; want one dup_blocks_cross_pkg violation failing the check",
			v, c.Failed())
	}
}

func TestCheckModuleRowAbsent(t *testing.T) {
	t.Parallel()

	one := 1
	t.Run("no module metrics", func(t *testing.T) {
		t.Parallel()
		c, _, err := Check(t.Context(), fakeTarget(""), CheckOptions{BaselineFile: writeFakeBaseline(t), All: true})
		if err != nil || c.Module != nil {
			t.Errorf("Check = module %+v, %v; want no module row from an extractor without one", c.Module, err)
		}
	})
	t.Run("module row fails", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("boom")
		c, failed, err := Check(t.Context(), moduleTarget(0, boom), CheckOptions{
			BaselineFile: writeModuleBaseline(t, &one),
			All:          true,
		})
		if err != nil || c.Module != nil || len(c.Packages) != 2 {
			t.Fatalf("Check = module %+v, %d packages, %v; want the packages and no module row", c.Module, len(c.Packages), err)
		}
		var pe *PackageError
		if len(failed) != 1 || !errors.As(failed[0], &pe) || pe.Path != metrics.ModuleRowID || !errors.Is(pe, boom) {
			t.Errorf("failed = %v, want one PackageError for %s wrapping boom", failed, metrics.ModuleRowID)
		}
	})
}

func TestCollectModuleRow(t *testing.T) {
	t.Parallel()

	tg := moduleTarget(3, nil)
	pkgs, err := baseline.Collect(t.Context(), tg.Ext, tg.Mod)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := pkgs[metrics.ModuleRowID]
	if !ok || m.DupBlocksCrossPkg == nil || *m.DupBlocksCrossPkg != 3 || len(pkgs) != 3 {
		t.Errorf("Collect = %d rows, module row %+v; want 2 packages and the module row at 3", len(pkgs), m)
	}
	if _, err := baseline.Collect(t.Context(), moduleTarget(0, errors.New("boom")).Ext, tg.Mod); err == nil {
		t.Error("Collect with a failing module row returned no error")
	}
}

// crossPackageTarget loads the fixture module with the default rules plus
// exactly one rule on dup_blocks_cross_pkg, max_delta 0 with
// ratchet_from_zero, and returns it with its collected rows, the module
// row included.
func crossPackageTarget(t *testing.T) (*Target, map[string]metrics.RawMetrics) {
	t.Helper()
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	// The default rules, whatever they say on dup_blocks_cross_pkg, plus
	// exactly one rule on it.
	zero := 0.0
	cfg.Thresholds = slices.DeleteFunc(cfg.Thresholds, func(r gate.Threshold) bool { return metrics.ModuleWide(r.Metric) })
	cfg.Thresholds = append(cfg.Thresholds, gate.Threshold{
		Metric: "dup_blocks_cross_pkg", Kind: gate.Density, MaxDelta: &zero, RatchetFromZero: true,
	})
	tg, err := LoadTarget(fixtureDir, TargetOptions{Config: cfg, Tokenizer: TokenizerEst})
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := baseline.Collect(t.Context(), tg.Ext, tg.Mod)
	if err != nil {
		t.Fatal(err)
	}
	return tg, pkgs
}

// TestCheckCrossPackageCopyOneFinding checks the fixture pair a and b, which
// share one block, against a baseline from before the copy: a rule on
// dup_blocks_cross_pkg yields exactly one violation, on the module row,
// while a and b still report their own count.
func TestCheckCrossPackageCopyOneFinding(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	tg, pkgs := crossPackageTarget(t)
	// Before the copy no row counted a cross-package block.
	for id, m := range pkgs {
		n := 0
		m.DupBlocksCrossPkg = &n
		pkgs[id] = m
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := baseline.Write(path, "", tg.Mod.ModulePath, TokenizerEst, pkgs); err != nil {
		t.Fatal(err)
	}

	c, failed, err := Check(t.Context(), tg, CheckOptions{BaselineFile: path, All: true})
	if err != nil || len(failed) != 0 {
		t.Fatalf("Check = (%v, %v), want no error", failed, err)
	}
	if c.Module == nil {
		t.Fatal("check has no module row")
	}
	type finding struct{ row, metric string }
	var got []finding
	rows := append([]report.CheckedPackage{*c.Module}, c.Packages...)
	for i := range rows {
		r := &rows[i].Report
		for _, v := range r.Violations {
			got = append(got, finding{r.PackagePath, v.Metric})
		}
		if (r.PackagePath == "a" || r.PackagePath == "b") &&
			(r.Metrics.DupBlocksCrossPkg == nil || *r.Metrics.DupBlocksCrossPkg != 1) {
			t.Errorf("%s: dup_blocks_cross_pkg = %v, want 1 reported", r.PackagePath, r.Metrics.DupBlocksCrossPkg)
		}
	}
	want := []finding{{metrics.ModuleRowID, "dup_blocks_cross_pkg"}}
	if !slices.Equal(got, want) {
		t.Fatalf("violations = %v, want exactly %v", got, want)
	}
	// The finding names both copies, a.Checksum and b.Digest, and is
	// located on the first for the GitHub annotation.
	v := c.Module.Report.Violations[0]
	const suggestion = "1 duplicate block is shared with other packages; " +
		"extract the shared block in a/a.go:7-19 and b/b.go:13-25 into one package."
	if v.Suggestion != suggestion {
		t.Errorf("suggestion = %q, want %q", v.Suggestion, suggestion)
	}
	if l := v.Location; l == nil || l.File != "a/a.go" || l.Line != 7 {
		t.Errorf("violation located at %+v, want a/a.go line 7", l)
	}
}

// TestCheckCrossPackageBaselineWithoutModuleRow checks the fixture pair a
// and b, which share one block, against a baseline file written before the
// module row existed: the shared block was already there, so the rule on
// dup_blocks_cross_pkg is skipped with one note instead of ratcheting from
// zero, and the module row still reports its count.
func TestCheckCrossPackageBaselineWithoutModuleRow(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	tg, pkgs := crossPackageTarget(t)
	var logs bytes.Buffer
	tg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	delete(pkgs, metrics.ModuleRowID)
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := baseline.Write(path, "", tg.Mod.ModulePath, TokenizerEst, pkgs); err != nil {
		t.Fatal(err)
	}

	c, failed, err := Check(t.Context(), tg, CheckOptions{BaselineFile: path, All: true})
	if err != nil || len(failed) != 0 {
		t.Fatalf("Check = (%v, %v), want no error", failed, err)
	}
	if c.Module == nil {
		t.Fatal("check has no module row")
	}
	var got []string
	rows := append([]report.CheckedPackage{*c.Module}, c.Packages...)
	for i := range rows {
		for _, v := range rows[i].Report.Violations {
			got = append(got, rows[i].Report.PackagePath+": "+v.Metric)
		}
	}
	if len(got) != 0 {
		t.Errorf("violations = %v, want none", got)
	}
	if m := c.Module.Report.Metrics.DupBlocksCrossPkg; m == nil || *m != 1 {
		t.Errorf("module row dup_blocks_cross_pkg = %v, want 1 reported", m)
	}
	if n := strings.Count(logs.String(), noModuleRowNote); n != 1 {
		t.Errorf("logged %d no-module-row notes, want 1; logs:\n%s", n, logs.String())
	}
}
