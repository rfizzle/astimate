package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

// Fixture copies relative to this package's directory; see testdata/go.
const (
	degradedDir = "../../testdata/go/fixture-degraded"
	grownDir    = "../../testdata/go/fixture-grown"
	extmodDir   = "../../testdata/go/extmod"
)

// degradedMetrics are the rules the degraded fixture's tested package
// breaks by adding a duplicate function, an untested export, a global and
// one function of cognitive complexity 40.
func degradedMetrics() []string {
	return []string{"changed_func_cognitive_max", "dup_blocks", "globals", "untested_exports"}
}

// degradedLocations maps each of degradedMetrics to the file, relative to
// the degraded fixture's tested package, and line its annotation lands on:
// the lines the degraded copy adds.
func degradedLocations() map[string]string {
	return map[string]string{
		"changed_func_cognitive_max": "grade.go,line=8",
		"dup_blocks":                 "degraded.go,line=15",
		"globals":                    "degraded.go,line=9",
		"untested_exports":           "degraded.go,line=13",
	}
}

// degradedGrade is the start of the changed_func_cognitive_max suggestion
// on the degraded fixture, naming its complex function.
const degradedGrade = "Changed function grade (grade.go:8) has cognitive complexity 40"

// fixtureBaseline extracts the pristine fixture and writes it as a baseline
// file with ref "fixture", functions included, in a temporary directory,
// returning its path.
func fixtureBaseline(t *testing.T) string {
	t.Helper()
	return writeFixtureBaseline(t, true)
}

// writeFixtureBaseline is fixtureBaseline, recording the fixture's
// functions only when withFunctions is set, as files written before
// function records existed do not.
func writeFixtureBaseline(t *testing.T, withFunctions bool) string {
	t.Helper()
	// Extract as check does, with the extractor the default config builds.
	tg, err := engine.LoadTarget(fixtureDir, engine.TargetOptions{Tokenizer: tokenizerEst})
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := baseline.Collect(context.Background(), tg.Ext, tg.Mod)
	if err != nil {
		t.Fatalf("collecting the fixture baseline: %v", err)
	}
	var funcs map[string][]metrics.FunctionInfo
	if withFunctions {
		funcs, err = baseline.CollectFunctions(context.Background(), tg.Ext, tg.Mod, pkgs)
		if err != nil {
			t.Fatalf("collecting the fixture functions: %v", err)
		}
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	err = baseline.WriteContents(path, baseline.Contents{
		Ref: "fixture", ModulePath: tg.Mod.ModulePath, Tokenizer: tokenizerEst, Packages: pkgs, Functions: funcs,
	})
	if err != nil {
		t.Fatalf("writing the fixture baseline: %v", err)
	}
	return path
}

// violationMetrics returns the sorted, distinct metrics of the violations
// of the report for path in reports.
func violationMetrics(reports []report.Report, path string) []string {
	var out []string
	for i := range reports {
		if reports[i].PackagePath != path {
			continue
		}
		for _, v := range reports[i].Violations {
			out = append(out, v.Metric)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// containsAll reports whether every element of want is in got.
func containsAll(got, want []string) bool {
	for _, w := range want {
		if !slices.Contains(got, w) {
			return false
		}
	}
	return true
}

// decodeReports decodes the check JSON array, rejecting unknown fields.
func decodeReports(t *testing.T, data []byte) []report.Report {
	t.Helper()
	var reports []report.Report
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&reports); err != nil {
		t.Fatalf("decoding check reports: %v\n%s", err, data)
	}
	return reports
}

func TestCheckFixtures(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	base := fixtureBaseline(t)
	fixtures := []struct {
		name     string
		dir      string
		wantCode int
	}{
		{"unchanged", fixtureDir, exitOK},
		{"grown", grownDir, exitOK},
		{"degraded", degradedDir, exitGateFailed},
	}
	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			t.Parallel()

			check := func(t *testing.T, format string) (stdout, stderr string) {
				t.Helper()
				var out, errOut bytes.Buffer
				args := []string{"check", fx.dir, "--all", "--baseline", base, "--format", format}
				want := fx.wantCode
				if format == formatHook {
					want = exitOK // the hook's JSON carries the decision (SPEC.md 8.5)
				}
				if got := run(args, &out, &errOut); got != want {
					t.Fatalf("run(%q) exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s",
						args, got, want, out.String(), errOut.String())
				}
				return out.String(), errOut.String()
			}
			failing := fx.wantCode == exitGateFailed

			t.Run("json", func(t *testing.T) {
				t.Parallel()

				out, _ := check(t, formatJSON)
				reports := decodeReports(t, []byte(out))
				paths := make([]string, 0, len(reports))
				for i := range reports {
					r := &reports[i]
					paths = append(paths, r.PackagePath)
					if r.Passed == nil || r.Baseline == nil || r.Baseline.Ref != "fixture" {
						t.Errorf("%s: passed %v baseline %+v, want a verdict against ref fixture", r.PackagePath, r.Passed, r.Baseline)
					}
				}
				// The module row comes first; a and b share one block in
				// every fixture copy, so it passes against the baseline.
				if want := append([]string{metrics.ModuleRowID}, fixturePackages()...); !slices.Equal(paths, want) {
					t.Errorf("reports = %q, want the module row and every fixture package %q", paths, want)
				}
				if m := reports[0]; m.PackagePath == metrics.ModuleRowID {
					if m.Passed == nil || !*m.Passed || m.Metrics.DupBlocksCrossPkg == nil || *m.Metrics.DupBlocksCrossPkg != 1 {
						t.Errorf("module row passed %v with dup_blocks_cross_pkg %v, want a pass at 1", m.Passed, m.Metrics.DupBlocksCrossPkg)
					}
				}
				got := violationMetrics(reports, "tested")
				if failing && !containsAll(got, degradedMetrics()) {
					t.Errorf("tested violations = %q, want %q among them", got, degradedMetrics())
				}
				if !failing && len(got) > 0 {
					t.Errorf("tested violations = %q, want none", got)
				}
				for i := range reports {
					r := &reports[i]
					// Only the module row has no function-level diff.
					if got := r.Metrics.ChangedFuncCognitiveMax; (got == nil) != (r.PackagePath == metrics.ModuleRowID) {
						t.Errorf("%s: changed_func_cognitive_max = %v, want null only on the module row", r.PackagePath, got)
					}
					if r.PackagePath != "tested" {
						continue
					}
					// The degraded copy adds grade (40) while the p90 stays
					// under 10; the grown copy adds simple functions, and
					// the unchanged copy changes none.
					got := r.Metrics.ChangedFuncCognitiveMax
					switch {
					case got == nil:
					case fx.name == "unchanged" && *got != 0,
						fx.name == "grown" && *got > 30,
						failing && (*got != 40 || r.Metrics.CognitiveP90 >= 10):
						t.Errorf("tested: changed_func_cognitive_max %d with cognitive_p90 %d", *got, r.Metrics.CognitiveP90)
					}
					for _, v := range r.Violations {
						if v.Metric == "changed_func_cognitive_max" && !strings.HasPrefix(v.Suggestion, degradedGrade) {
							t.Errorf("changed_func_cognitive_max suggestion = %q, want it to start %q", v.Suggestion, degradedGrade)
						}
						// Each degraded violation carries the location its
						// github annotation lands on, module-relative.
						want, ok := degradedLocations()[v.Metric]
						if !ok {
							continue
						}
						file, line, _ := strings.Cut(want, ",line=")
						want = "tested/" + file + ":" + line
						if l := v.Location; l == nil || l.File+":"+strconv.Itoa(l.Line) != want {
							t.Errorf("%s location = %+v, want %s", v.Metric, l, want)
						}
					}
				}
			})
			t.Run("text", func(t *testing.T) {
				t.Parallel()

				out, _ := check(t, formatText)
				if !strings.Contains("\n"+out, "\n<module>: dup_blocks_cross_pkg 1, 0 violations, 0 warnings\n") {
					t.Errorf("text has no module summary line:\n%s", out)
				}
				for _, pkg := range fixturePackages() {
					if !strings.Contains(out, "\n"+pkg+": ") && !strings.HasPrefix(out, pkg+": ") {
						t.Errorf("text has no summary line for %s:\n%s", pkg, out)
					}
				}
				for _, m := range degradedMetrics() {
					if got := strings.Contains(out, "    "+m+": "); got != failing {
						t.Errorf("text names %s = %v, want %v:\n%s", m, got, failing, out)
					}
				}
				if got := strings.Contains(out, degradedGrade); got != failing {
					t.Errorf("text names the complex function = %v, want %v:\n%s", got, failing, out)
				}
			})
			t.Run("hook", func(t *testing.T) {
				t.Parallel()

				out, _ := check(t, formatHook)
				if !failing {
					if out != "{}\n" {
						t.Errorf("hook stdout = %q, want %q", out, "{}\n")
					}
					return
				}
				dec := json.NewDecoder(strings.NewReader(out))
				dec.DisallowUnknownFields()
				var got struct {
					Decision string `json:"decision"`
					Reason   string `json:"reason"`
				}
				if err := dec.Decode(&got); err != nil {
					t.Fatalf("decoding hook output %q: %v", out, err)
				}
				if _, err := dec.Token(); err != io.EOF {
					t.Errorf("hook stdout holds more than one JSON object: %q", out)
				}
				if got.Decision != "block" {
					t.Errorf("decision = %q, want block", got.Decision)
				}
				for _, m := range degradedMetrics() {
					if !strings.Contains(got.Reason, m+": ") {
						t.Errorf("reason does not list %s:\n%s", m, got.Reason)
					}
				}
			})
			t.Run("github", func(t *testing.T) {
				t.Parallel()

				reports := decodeReports(t, []byte(func() string { out, _ := check(t, formatJSON); return out }()))
				out, _ := check(t, formatGitHub)
				var errs []string
				for line := range strings.SplitSeq(strings.TrimSuffix(out, "\n"), "\n") {
					if strings.HasPrefix(line, "::error ") {
						errs = append(errs, line)
					}
				}
				want := 0
				for i := range reports {
					want += len(reports[i].Violations)
				}
				if len(errs) != want {
					t.Errorf("github has %d ::error annotations, want one per violation (%d):\n%s", len(errs), want, out)
				}
				// Annotation paths are relative to the repository holding
				// the fixture, so each lands on the line the degraded copy
				// added.
				abs, err := filepath.Abs(fx.dir)
				if err != nil {
					t.Fatal(err)
				}
				tested := path.Join(baseline.RepoDir(t.Context(), abs), "tested")
				for m, loc := range degradedLocations() {
					got := strings.Contains(out, "::error file="+tested+"/"+loc+"::"+m+": ")
					if got != failing {
						t.Errorf("github annotates %s on %s/%s = %v, want %v:\n%s", m, tested, loc, got, failing, out)
					}
				}
			})
		})
	}
}

// rootCountingExtractor wraps the Go extractor and records the roots it lists
// and extracts, so a test can prove each tree is loaded once. Embedding
// keeps Details, so suggestions still name identifiers.
type rootCountingExtractor struct {
	*golang.Extractor

	mu       sync.Mutex
	packages map[string]int
	extracts map[string]bool
}

func newRootCountingExtractor(ext *golang.Extractor) *rootCountingExtractor {
	return &rootCountingExtractor{Extractor: ext, packages: map[string]int{}, extracts: map[string]bool{}}
}

func (c *rootCountingExtractor) Packages(root string) ([]string, error) {
	c.mu.Lock()
	c.packages[root]++
	c.mu.Unlock()
	return c.Extractor.Packages(root)
}

func (c *rootCountingExtractor) Extract(ctx context.Context, mod *metrics.ModuleContext, pkg string) (metrics.RawMetrics, error) {
	c.mu.Lock()
	c.extracts[mod.Root] = true
	c.mu.Unlock()
	return c.Extractor.Extract(ctx, mod, pkg)
}

// copyTree copies the regular files under src to dst, skipping any
// directory named golden.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if d.Name() == "golden" {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatalf("copying %s to %s: %v", src, dst, err)
	}
}

// gitIn runs git in dir with user and system config ignored, so the test
// does not depend on the machine's identity or signing settings.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Drop variables a git hook may set that redirect git elsewhere.
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "GIT_DIR=") || strings.HasPrefix(kv, "GIT_WORK_TREE=") ||
			strings.HasPrefix(kv, "GIT_INDEX_FILE=")
	})
	env = append(env,
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// TestCheckJSONModuleLocation checks the module row of check --format
// json against a baseline without the fixture's one cross-package block:
// its dup_blocks_cross_pkg violation carries the location of the block's
// first occurrence, and its details list the block.
func TestCheckJSONModuleLocation(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	tg, err := engine.LoadTarget(fixtureDir, engine.TargetOptions{Tokenizer: tokenizerEst})
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := baseline.Collect(t.Context(), tg.Ext, tg.Mod)
	if err != nil {
		t.Fatalf("collecting the fixture baseline: %v", err)
	}
	zero := 0
	row := pkgs[metrics.ModuleRowID]
	row.DupBlocksCrossPkg = &zero
	pkgs[metrics.ModuleRowID] = row
	base := filepath.Join(t.TempDir(), "baseline.json")
	err = baseline.WriteContents(base, baseline.Contents{
		Ref: "fixture", ModulePath: tg.Mod.ModulePath, Tokenizer: tokenizerEst, Packages: pkgs,
	})
	if err != nil {
		t.Fatalf("writing the baseline: %v", err)
	}
	// No default rule gates dup_blocks_cross_pkg (SPEC.md 8.1); add one.
	cfg := filepath.Join(t.TempDir(), "astimate.yaml")
	rule := "\nlanguages:\n  go:\n    thresholds:\n" +
		"      - metric: dup_blocks_cross_pkg\n        kind: density\n        max_delta: 0\n        ratchet_from_zero: true\n"
	if err := os.WriteFile(cfg, append(config.Default(), rule...), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	args := []string{"check", fixtureDir, "--all", "--config", cfg, "--baseline", base, "--format", formatJSON}
	if got := run(args, &stdout, &stderr); got != exitGateFailed {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr:\n%s", args, got, exitGateFailed, stderr.String())
	}
	reports := decodeReports(t, stdout.Bytes())
	if len(reports) == 0 || reports[0].PackagePath != metrics.ModuleRowID {
		t.Fatalf("reports do not start with the module row:\n%s", stdout.String())
	}
	m := &reports[0]
	if len(m.Violations) != 1 || m.Violations[0].Metric != "dup_blocks_cross_pkg" {
		t.Fatalf("module violations = %+v, want one on dup_blocks_cross_pkg", m.Violations)
	}
	if l := m.Violations[0].Location; l == nil || *l != (report.Location{File: "a/a.go", Line: 7}) {
		t.Errorf("module finding location = %+v, want a/a.go line 7", l)
	}
	want := []report.CrossOccurrence{
		{Package: "a", File: "a/a.go", StartLine: 7, EndLine: 19},
		{Package: "b", File: "b/b.go", StartLine: 13, EndLine: 25},
	}
	if m.Details == nil || len(m.Details.CrossBlocks) != 1 || !slices.Equal(m.Details.CrossBlocks[0].Occurrences, want) {
		t.Errorf("module details = %+v, want the one block %+v", m.Details, want)
	}
}

// TestChangedFunctionNeedsBaseline checks that changed_func_cognitive_max
// is null, and its rule skipped, wherever there is no function-level
// baseline: in assess, and in a check against a baseline file written
// before function records existed, which says so on stderr.
func TestChangedFunctionNeedsBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	t.Run("assess", func(t *testing.T) {
		t.Parallel()
		var out, errOut bytes.Buffer
		args := []string{"assess", "--json", filepath.Join(degradedDir, "tested")}
		if got := run(args, &out, &errOut); got != exitOK {
			t.Fatalf("run(%q) = %d; stderr:\n%s", args, got, errOut.String())
		}
		var r report.Report
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if r.Metrics.ChangedFuncCognitiveMax != nil {
			t.Errorf("assess changed_func_cognitive_max = %d, want null", *r.Metrics.ChangedFuncCognitiveMax)
		}
	})
	t.Run("baseline file without functions", func(t *testing.T) {
		t.Parallel()
		base := writeFixtureBaseline(t, false)
		var out, errOut bytes.Buffer
		args := []string{"check", degradedDir, "--all", "--baseline", base, "--format", formatJSON}
		if got := run(args, &out, &errOut); got != exitGateFailed {
			t.Fatalf("run(%q) = %d, want the other degraded rules to fail; stderr:\n%s", args, got, errOut.String())
		}
		reports := decodeReports(t, out.Bytes())
		for i := range reports {
			if got := reports[i].Metrics.ChangedFuncCognitiveMax; got != nil {
				t.Errorf("%s: changed_func_cognitive_max = %d, want null", reports[i].PackagePath, *got)
			}
		}
		if got := violationMetrics(reports, "tested"); slices.Contains(got, "changed_func_cognitive_max") {
			t.Errorf("tested violations = %q, want the rule skipped", got)
		}
		if n := strings.Count(errOut.String(), "metric=changed_func_cognitive_max reason="); n != 1 {
			t.Errorf("stderr notes the skipped rule %d times, want once:\n%s", n, errOut.String())
		}
	})
}

func TestCheckGitRef(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages and runs git")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Parallel()

	// The repository holds the fixture and the module its replace directive
	// points at, so the baseline worktree resolves ../extmod too.
	repo := t.TempDir()
	copyTree(t, fixtureDir, filepath.Join(repo, "fixture"))
	copyTree(t, extmodDir, filepath.Join(repo, "extmod"))
	gitIn(t, repo, "init", "-q", "-b", "master")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "--no-verify", "-m", "pristine fixture")
	copyTree(t, filepath.Join(degradedDir, "tested"), filepath.Join(repo, "fixture", "tested"))

	tg, err := engine.LoadTarget(filepath.Join(repo, "fixture"), engine.TargetOptions{Tokenizer: tokenizerEst})
	if err != nil {
		t.Fatal(err)
	}
	ext := newRootCountingExtractor(tg.Ext.(*golang.Extractor))
	tg.Ext = ext
	var stdout, stderr bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&stderr, nil))
	opts := checkOptions{base: "master", format: formatJSON}
	tg.Logger = logger
	if got := checkTarget(context.Background(), tg, opts, &stdout, &stderr, logger); got != exitGateFailed {
		t.Fatalf("exit code = %d, want %d; stderr = %s", got, exitGateFailed, stderr.String())
	}

	reports := decodeReports(t, stdout.Bytes())
	if len(reports) != 2 || reports[0].PackagePath != metrics.ModuleRowID || reports[1].PackagePath != "tested" {
		paths := make([]string, 0, len(reports))
		for i := range reports {
			paths = append(paths, reports[i].PackagePath)
		}
		t.Fatalf("checked %q, want the module row and tested", paths)
	}
	if got := violationMetrics(reports, "tested"); !containsAll(got, degradedMetrics()) {
		t.Errorf("tested violations = %q, want %q among them", got, degradedMetrics())
	}
	for i := range reports {
		if b := reports[i].Baseline; b == nil || len(b.Ref) != 40 {
			t.Errorf("%s: baseline = %+v, want the merge-base commit", reports[i].PackagePath, b)
		}
	}

	// Two trees, head and the baseline worktree, each listed exactly once;
	// the Go extractor loads a root once and serves Packages and Extract
	// from that load, so this is two packages.Load calls.
	if len(ext.packages) != 2 {
		t.Errorf("Packages called for roots %v, want head and baseline", ext.packages)
	}
	for root, n := range ext.packages {
		if n != 1 {
			t.Errorf("Packages(%s) called %d times, want 1", root, n)
		}
	}
	for root := range ext.extracts {
		if _, ok := ext.packages[root]; !ok {
			t.Errorf("Extract ran on %s, a root never listed", root)
		}
	}
}

// TestCheckStaged checks that --staged extracts head from a temporary copy
// of the index, removed afterwards, so an unstaged degradation is ignored
// until it is staged, that --all --staged checks every package of the
// index, and that --staged outside git is an analysis failure saying why.
func TestCheckStaged(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages and runs git")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Parallel()

	repo := t.TempDir()
	copyTree(t, fixtureDir, filepath.Join(repo, "fixture"))
	copyTree(t, extmodDir, filepath.Join(repo, "extmod"))
	gitIn(t, repo, "init", "-q", "-b", "master")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "--no-verify", "-m", "pristine fixture")
	copyTree(t, filepath.Join(degradedDir, "tested"), filepath.Join(repo, "fixture", "tested"))

	// check runs the staged check of the fixture and returns its exit code,
	// reports and stderr, and the roots head was listed at.
	check := func(t *testing.T, all bool) (int, []report.Report, string, map[string]int) {
		t.Helper()
		tg, err := engine.LoadTarget(filepath.Join(repo, "fixture"), engine.TargetOptions{Tokenizer: tokenizerEst})
		if err != nil {
			t.Fatal(err)
		}
		ext := newRootCountingExtractor(tg.Ext.(*golang.Extractor))
		tg.Ext = ext
		var stdout, stderr bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&stderr, nil))
		tg.Logger = logger
		opts := checkOptions{base: "master", all: all, staged: true, format: formatJSON}
		code := checkTarget(context.Background(), tg, opts, &stdout, &stderr, logger)
		if strings.Contains(stdout.String(), "astimate-staged-") {
			t.Errorf("output names the temporary staged tree:\n%s", stdout.String())
		}
		for root := range ext.extracts {
			if strings.HasPrefix(root, repo) {
				t.Errorf("Extract ran on the working tree %s, want the staged copy", root)
			}
			if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("head root %s still exists after the check: %v", root, err)
			}
		}
		return code, decodeReports(t, stdout.Bytes()), stderr.String(), ext.packages
	}

	t.Run("unstaged degradation", func(t *testing.T) {
		code, reports, stderr, _ := check(t, false)
		if code != exitOK || len(reports) != 0 {
			t.Errorf("exit code = %d with %d reports, want %d and nothing selected; stderr = %s",
				code, len(reports), exitOK, stderr)
		}
	})
	t.Run("all", func(t *testing.T) {
		code, reports, stderr, _ := check(t, true)
		if code != exitOK || len(reports) < 2 {
			t.Fatalf("exit code = %d with %d reports, want %d and every package; stderr = %s",
				code, len(reports), exitOK, stderr)
		}
		if got := violationMetrics(reports, "tested"); len(got) != 0 {
			t.Errorf("tested violations = %q, want none: the degradation is not staged", got)
		}
	})

	gitIn(t, repo, "add", "fixture/tested")
	code, reports, stderr, packages := check(t, false)
	if code != exitGateFailed {
		t.Fatalf("staged degradation: exit code = %d, want %d; stderr = %s", code, exitGateFailed, stderr)
	}
	if got := violationMetrics(reports, "tested"); !containsAll(got, degradedMetrics()) {
		t.Errorf("tested violations = %q, want %q among them", got, degradedMetrics())
	}
	// Head is listed once, at the staged copy; the baseline worktree is
	// the other root.
	if len(packages) != 2 {
		t.Errorf("Packages called for roots %v, want the staged copy and the baseline", packages)
	}

	t.Run("outside git", func(t *testing.T) {
		t.Parallel()
		root := writeModule(t)
		var out, errOut bytes.Buffer
		if got := run([]string{"baseline", "write", root}, &out, &errOut); got != exitOK {
			t.Fatalf("baseline write exit code = %d; stderr = %s", got, errOut.String())
		}
		out.Reset()
		errOut.Reset()
		if got := run([]string{"check", root, "--staged"}, &out, &errOut); got != exitAnalysis {
			t.Errorf("exit code = %d, want %d", got, exitAnalysis)
		}
		if !strings.Contains(errOut.String(), "not in a git repository") || out.Len() != 0 {
			t.Errorf("stdout = %q, stderr = %q, want empty stdout and the missing repository named",
				out.String(), errOut.String())
		}
	})
}

func TestCheckDefaultRefFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	t.Run("baseline file", func(t *testing.T) {
		t.Parallel()

		// Outside git no default ref exists, so check falls back to the
		// module's baseline file, which records no ref, and checks every
		// package against it.
		root := writeModule(t)
		var out, errOut bytes.Buffer
		if got := run([]string{"baseline", "write", root}, &out, &errOut); got != exitOK {
			t.Fatalf("baseline write exit code = %d; stderr = %s", got, errOut.String())
		}
		out.Reset()
		errOut.Reset()
		if got := run([]string{"check", root, "--format", formatJSON}, &out, &errOut); got != exitOK {
			t.Fatalf("exit code = %d, want %d; stderr = %s", got, exitOK, errOut.String())
		}
		if !strings.Contains(errOut.String(), "using the baseline file") {
			t.Errorf("stderr = %q, want it to say the baseline file is used", errOut.String())
		}
		if reports := decodeReports(t, out.Bytes()); len(reports) != 3 {
			t.Errorf("checked %d rows, want the module row and 2 packages", len(reports))
		}
	})
	t.Run("no baseline", func(t *testing.T) {
		t.Parallel()

		root := writeModule(t)
		var out, errOut bytes.Buffer
		if got := run([]string{"check", root}, &out, &errOut); got != exitAnalysis {
			t.Errorf("exit code = %d, want %d", got, exitAnalysis)
		}
		if strings.Count(errOut.String(), "pass --base <ref> or --baseline <file>") != 1 || out.Len() != 0 {
			t.Errorf("stdout = %q, stderr = %q, want empty stdout and the ref guidance once", out.String(), errOut.String())
		}
	})
}

func TestCheckUsageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantCode int
		want     string
	}{
		{"base and baseline", []string{"--base", "main", "--baseline", "b.json"}, exitUsage, "mutually exclusive"},
		{"unknown format", []string{"--format", "xml"}, exitUsage, "unknown format"},
		{"unknown tokenizer", []string{"--tokenizer", "bpe"}, exitUsage, "unknown tokenizer"},
		{"two roots", []string{"a", "b"}, exitUsage, "want at most one module root"},
		{"unknown flag", []string{"--nope"}, exitUsage, "flag provided but not defined"},
		{"thresholds alias", []string{fixtureDir, "--thresholds", "missing.yaml"}, exitAnalysis, "reading config"},
		{"missing baseline file", []string{fixtureDir, "--baseline", "missing.json"}, exitAnalysis, "reading baseline"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			if got := run(append([]string{"check"}, tt.args...), &stdout, &stderr); got != tt.wantCode {
				t.Errorf("exit code = %d, want %d; stderr = %s", got, tt.wantCode, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.want)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

func TestCheckExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		format     string
		violations bool
		failed     int
		want       int
	}{
		{"clean", formatText, false, 0, exitOK},
		{"violation", formatText, true, 0, exitGateFailed},
		{"violation in json", formatJSON, true, 0, exitGateFailed},
		{"violation in github", formatGitHub, true, 0, exitGateFailed},
		{"extraction failure", formatText, false, 1, exitAnalysis},
		{"violation outranks extraction failure", formatText, true, 2, exitGateFailed},
		{"hook block decision exits 0", formatHook, true, 0, exitOK},
		{"hook block decision with extraction failure exits 0", formatHook, true, 1, exitOK},
		{"hook extraction failure without violations", formatHook, false, 1, exitAnalysis},
		{"hook clean", formatHook, false, 0, exitOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := checkExitCode(tt.format, tt.violations, tt.failed); got != tt.want {
				t.Errorf("checkExitCode(%s, %v, %d) = %d, want %d", tt.format, tt.violations, tt.failed, got, tt.want)
			}
		})
	}
}

func TestStopHookActive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input io.Reader
		want  bool
	}{
		{"true", strings.NewReader(`{"hook_event_name":"Stop","stop_hook_active":true}`), true},
		{"false", strings.NewReader(`{"hook_event_name":"Stop","stop_hook_active":false}`), false},
		{"field absent", strings.NewReader(`{"hook_event_name":"Stop"}`), false},
		{"no input", nil, false},
		{"empty input", strings.NewReader(""), false},
		{"malformed", strings.NewReader(`{"stop_hook_active": tru`), false},
		{"wrong type", strings.NewReader(`{"stop_hook_active":"true"}`), false},
		{"oversized", io.MultiReader(strings.NewReader(`{"stop_hook_active":true,"pad":"`),
			strings.NewReader(strings.Repeat("x", maxHookInput)), strings.NewReader(`"}`)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := stopHookActive(tt.input); got != tt.want {
				t.Errorf("stopHookActive = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckHookStdin(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	base := fixtureBaseline(t)
	tests := []struct {
		name  string
		stdin func() io.Reader
		allow bool
	}{
		{"stop_hook_active true", func() io.Reader { return strings.NewReader(`{"stop_hook_active":true}`) }, true},
		{"stop_hook_active false", func() io.Reader { return strings.NewReader(`{"stop_hook_active":false}`) }, false},
		{"terminal", func() io.Reader { return nil }, false},
		{"malformed", func() io.Reader { return strings.NewReader("not json") }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			args := []string{"--all", "--baseline", base, "--format", "hook", degradedDir}
			if got := runCheckInput(args, tt.stdin, &stdout, &stderr); got != exitOK {
				t.Fatalf("exit code = %d, want %d\nstderr:\n%s", got, exitOK, stderr.String())
			}
			if tt.allow {
				if stdout.String() != "{}\n" {
					t.Errorf("stdout = %q, want %q", stdout.String(), "{}\n")
				}
				if !strings.Contains(stderr.String(), "already blocked once") {
					t.Errorf("stderr does not say the hook already blocked:\n%s", stderr.String())
				}
				return
			}
			assertHookBlock(t, stdout.String(), degradedMetrics())
		})
	}

	t.Run("other formats ignore stdin", func(t *testing.T) {
		t.Parallel()

		var stdout, stderr bytes.Buffer
		read := false
		stdin := func() io.Reader {
			read = true
			return strings.NewReader(`{"stop_hook_active":true}`)
		}
		args := []string{"--all", "--baseline", base, "--format", "json", degradedDir}
		if got := runCheckInput(args, stdin, &stdout, &stderr); got != exitGateFailed || read {
			t.Errorf("json format: exit code = %d, stdin read = %v; want %d and no read", got, read, exitGateFailed)
		}
	})
}
