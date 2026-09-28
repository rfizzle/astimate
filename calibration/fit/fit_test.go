package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/fit/internal/report"
	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/metrics"
)

// dataPath is the committed corpus data the shipped thresholds are fitted
// from: the standard library and the cloned modules of corpus.yaml, with
// per-function cognitive counts.
const dataPath = "../data/2026-09-28-corpus/packages.jsonl"

// modulesPath is the committed module rows of the cloned corpus modules,
// which dup_blocks_cross_pkg is fitted from.
const modulesPath = "../data/2026-09-28-modules/modules.jsonl"

// previousDataPath is the earlier corpus data, collected before the rows
// carried per-function counts.
const previousDataPath = "../data/2026-09-27-corpus/packages.jsonl"

// previousCandidate is the candidate fitted from previousDataPath.
const previousCandidate = "../thresholds/astimate-thresholds-2026-09-27.yaml"

// uncalibratedBase is the base the committed candidates are fitted on.
const uncalibratedBase = "../../configs/uncalibrated.yaml"

// stdlibDataPath is the earlier standard-library-only data, kept for
// comparison; a fit of it carries the provisional suffix.
const stdlibDataPath = "../data/2026-09-27/packages.jsonl"

// committedCandidate is the candidate fitted from dataPath, which the
// embedded default carries.
const committedCandidate = "../thresholds/astimate-thresholds-2026-09-28.yaml"

// shippedVersion is committedCandidate's config_version.
const shippedVersion = "thresholds-2026-09-28"

// TestFitModulesFile fits the committed data with the committed module
// rows and checks the report's cross-package section and the fitted
// dup_blocks_cross_pkg rule, and that a modules file holding a package row
// is rejected.
func TestFitModulesFile(t *testing.T) {
	dir := t.TempDir()
	res, err := fit(options{data: dataPath, modules: modulesPath, base: uncalibratedBase,
		out: filepath.Join(dir, "c.yaml"), report: filepath.Join(dir, "r.md"), date: "2026-09-28", suffix: suffixAuto})
	if err != nil {
		t.Fatalf("fit: %v", err)
	}
	data, err := os.ReadFile(res.report)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"- Module rows: `" + modulesPath + "`, 36 `<module>` rows", "## Cross-package duplication",
		"| Modules | p25 | p50 | p75 | p90 | max |", "| 36 | 1 | 9 | 104 | 443 | 3394 |", "### `dup_blocks_cross_pkg` (density)",
		"Rows: 36, module rows."} {
		if !strings.Contains(string(data), want) {
			t.Errorf("report does not contain %q", want)
		}
	}
	cfg, err := config.Load(res.out)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range cfg.Thresholds {
		if r.Metric == "dup_blocks_cross_pkg" && (report.Opt(r.Max) != "450" || report.Opt(r.MaxDelta) != "0" || r.RatchetFromZero) {
			t.Errorf("dup_blocks_cross_pkg max %s max_delta %s ratchet %t, want 450, 0, false", report.Opt(r.Max), report.Opt(r.MaxDelta), r.RatchetFromZero)
		}
	}
	_, err = fit(options{data: dataPath, modules: dataPath, base: uncalibratedBase,
		out: filepath.Join(dir, "c2.yaml"), report: filepath.Join(dir, "r2.md"), date: "2026-09-28", suffix: suffixAuto})
	if err == nil || !strings.Contains(err.Error(), "not a module row") {
		t.Errorf("fit with package rows as modules: err = %v, want a module-row error", err)
	}
}

// fitInto fits the committed data into dir and returns the candidate's
// path and the report.
func fitInto(t *testing.T, dir string) (candidate, reportPath string) {
	t.Helper()
	opts := options{
		data:    dataPath,
		modules: modulesPath,
		out:     filepath.Join(dir, "candidate.yaml"),
		report:  filepath.Join(dir, "report.md"),
		date:    "2026-09-28",
		suffix:  suffixAuto,
		base:    uncalibratedBase,
	}
	res, err := fit(opts)
	if err != nil {
		t.Fatalf("fit: %v", err)
	}
	if res.version != shippedVersion {
		t.Errorf("version = %q, want %q with no suffix", res.version, shippedVersion)
	}
	data, err := os.ReadFile(res.report)
	if err != nil {
		t.Fatal(err)
	}
	return res.out, string(data)
}

// TestCandidate fits the committed data and checks the candidate validates,
// keeps every non-threshold key of the base, and is described for every
// gated metric by the report.
func TestCandidate(t *testing.T) {
	path, rendered := fitInto(t, t.TempDir())
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("candidate does not validate: %v", err)
	}
	base, err := config.Load(uncalibratedBase)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Rebuild != base.Rebuild || cfg.CharsPerToken != base.CharsPerToken || cfg.Duplication != base.Duplication {
		t.Errorf("candidate changed the rebuild or extraction settings")
	}
	if len(cfg.Thresholds) != len(base.Thresholds) {
		t.Fatalf("candidate has %d rules, base %d", len(cfg.Thresholds), len(base.Thresholds))
	}
	for i, r := range cfg.Thresholds {
		b := base.Thresholds[i]
		if r.Metric != b.Metric || r.Kind != b.Kind || r.WarnAt != b.WarnAt || r.RatchetFromZero != b.RatchetFromZero ||
			(r.Max == nil) != (b.Max == nil) || (r.MaxDelta == nil) != (b.MaxDelta == nil) {
			t.Errorf("thresholds[%d] %s: shape changed from the base", i, r.Metric)
		}
		if !strings.Contains(rendered, "### `"+r.Metric+"` ("+string(r.Kind)+")") {
			t.Errorf("report has no section for %s", r.Metric)
		}
	}
	for _, want := range []string{"cloned-module rows only", "max_delta stays 0 on", "## Cross-package duplication",
		"## Per-function cognitive complexity", "| Functions | p50 | p90 | p99 | max |"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("report does not contain %q", want)
		}
	}
	if strings.Contains(rendered, "**Provisional.**") {
		t.Error("report calls the corpus fit provisional")
	}
}

// TestPreviousDataKeepsFunctionBase fits the earlier corpus data, whose
// rows carry no per-function counts, and checks changed_func_cognitive_max
// keeps the base max and the report says why, while the comparison with
// the earlier candidate finds nothing moved: the fit reproduces it.
func TestPreviousDataKeepsFunctionBase(t *testing.T) {
	dir := t.TempDir()
	res, err := fit(options{data: previousDataPath, base: uncalibratedBase, compare: previousCandidate,
		out: filepath.Join(dir, "c.yaml"), report: filepath.Join(dir, "r.md"), date: "2026-09-27", suffix: suffixAuto})
	if err != nil {
		t.Fatalf("fit: %v", err)
	}
	data, err := os.ReadFile(res.report)
	if err != nil {
		t.Fatal(err)
	}
	// The earlier data has no module rows either, and dup_blocks_cross_pkg
	// keeps the base's placeholder, which the earlier candidate lacked.
	for _, want := range []string{"`changed_func_cognitive_max`: no row counts its functions", "## Against thresholds-2026-09-27",
		"`dup_blocks_cross_pkg`: the data has no `<module>` rows", "Only `dup_blocks_cross_pkg` moved"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("report does not contain %q", want)
		}
	}
}

// TestCompareNamesMovedRules fits the committed data and module rows
// against the earlier candidate and checks the report names
// changed_func_cognitive_max and the new dup_blocks_cross_pkg rule as the
// only rules that moved.
func TestCompareNamesMovedRules(t *testing.T) {
	dir := t.TempDir()
	res, err := fit(options{data: dataPath, modules: modulesPath, base: uncalibratedBase, compare: previousCandidate,
		out: filepath.Join(dir, "c.yaml"), report: filepath.Join(dir, "r.md"), date: "2026-09-28", suffix: suffixAuto})
	if err != nil {
		t.Fatalf("fit: %v", err)
	}
	data, err := os.ReadFile(res.report)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Only `changed_func_cognitive_max`, `dup_blocks_cross_pkg` moved; every other limit is unchanged."; !strings.Contains(string(data), want) {
		t.Errorf("report does not contain %q", want)
	}
}

// TestStdlibOnlyIsProvisional checks a fit of standard-library rows alone
// carries the provisional suffix.
func TestStdlibOnlyIsProvisional(t *testing.T) {
	dir := t.TempDir()
	res, err := fit(options{data: stdlibDataPath, out: filepath.Join(dir, "c.yaml"), report: filepath.Join(dir, "r.md"),
		date: "2026-09-27", suffix: suffixAuto})
	if err != nil {
		t.Fatalf("fit: %v", err)
	}
	if res.version != "thresholds-2026-09-27-stdlib-provisional" {
		t.Errorf("version = %q, want the standard-library provisional suffix", res.version)
	}
}

// TestCommittedCandidate checks the committed candidate validates.
func TestCommittedCandidate(t *testing.T) {
	cfg, err := config.Load(committedCandidate)
	if err != nil {
		t.Fatalf("committed candidate does not validate: %v", err)
	}
	if cfg.Version != shippedVersion {
		t.Errorf("config_version = %q, want %q", cfg.Version, shippedVersion)
	}
}

// TestDefaultIsCommittedCandidate checks the embedded default carries the
// committed candidate's config_version and exactly its rules, so the
// shipped thresholds are the fitted ones.
func TestDefaultIsCommittedCandidate(t *testing.T) {
	cand, err := config.Load(committedCandidate)
	if err != nil {
		t.Fatal(err)
	}
	def, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	if def.Version != cand.Version {
		t.Errorf("default config_version = %q, candidate %q", def.Version, cand.Version)
	}
	if !reflect.DeepEqual(def.Thresholds, cand.Thresholds) {
		t.Errorf("default thresholds differ from the committed candidate:\n%+v\n%+v", def.Thresholds, cand.Thresholds)
	}
	if def.Rebuild != cand.Rebuild || def.CharsPerToken != cand.CharsPerToken || def.Duplication != cand.Duplication {
		t.Error("default rebuild or extraction settings differ from the committed candidate")
	}
}

// TestCandidatePassesGoodCode is the acceptance check: under the fitted
// candidate the fixture passes check --all against its own baseline, and
// the standard-library sort package passes against itself and, as a
// package new at head, every rule but the zero-tolerance ratchets, which
// judge what a change adds rather than where a package sits in the corpus.
// (errors, used before, sits above the corpus p90 of cognitive_p90 and
// fails the fitted max as a new package, as a tenth of the corpus does by
// construction.)
func TestCandidatePassesGoodCode(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the fixture and the standard library")
	}
	path, _ := fitInto(t, t.TempDir())
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	t.Run("fixture self-baseline", func(t *testing.T) {
		tg, err := engine.LoadTarget("../../testdata/go/fixture", engine.TargetOptions{Config: cfg})
		if err != nil {
			t.Fatal(err)
		}
		pkgs, err := baseline.Collect(ctx, tg.Ext, tg.Mod)
		if err != nil {
			t.Fatal(err)
		}
		self := filepath.Join(t.TempDir(), "baseline.json")
		if err := baseline.Write(self, "self", tg.Mod.ModulePath, baseline.DefaultTokenizer, pkgs); err != nil {
			t.Fatal(err)
		}
		c, failed, err := engine.Check(ctx, tg, engine.CheckOptions{BaselineFile: self, All: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(failed) > 0 {
			t.Fatalf("packages failed to extract: %v", failed)
		}
		if len(c.Packages) == 0 {
			t.Fatal("check selected no packages")
		}
		for _, p := range c.Packages {
			for _, v := range p.Report.Violations {
				t.Errorf("%s: violation %+v", p.Report.PackagePath, v)
			}
		}
	})

	t.Run("sort", func(t *testing.T) {
		m, err := golang.ExtractStdlib(ctx, "sort",
			golang.WithCharsPerToken(cfg.CharsPerToken),
			golang.WithDupMinTokens(cfg.Duplication.MinTokens),
			golang.WithDupIgnoreLiteralOnly(cfg.Duplication.IgnoreLiteralOnly),
			golang.WithDupFoldSigns(cfg.Duplication.FoldSigns))
		if err != nil {
			t.Skipf("toolchain cannot load the standard library: %v", err)
		}
		for _, tt := range []struct {
			name string
			base *metrics.RawMetrics
		}{{"self-baseline", &m}, {"new package", nil}} {
			res := gate.Evaluate(m, tt.base, cfg.Thresholds, nil)
			for _, v := range res.Violations {
				if tt.base == nil && v.Limit == "max_delta +0" {
					continue
				}
				t.Errorf("%s: violation %+v", tt.name, v)
			}
		}
	})
}

func TestRunUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"--data", "x", "--date", "27-09-2026"}, {"--data", "x", "extra"}} {
		if got := run(args, io.Discard, io.Discard); got != 2 {
			t.Errorf("run(%q) = %d, want 2", args, got)
		}
	}
}
