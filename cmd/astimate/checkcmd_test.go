package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
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
// breaks by adding a duplicate function, an untested export and a global.
func degradedMetrics() []string {
	return []string{"dup_blocks", "globals", "untested_exports"}
}

// fixtureBaseline extracts the pristine fixture and writes it as a baseline
// file with ref "fixture" in a temporary directory, returning its path.
func fixtureBaseline(t *testing.T) string {
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
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := baseline.Write(path, "fixture", tg.Mod.ModulePath, tokenizerEst, pkgs); err != nil {
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
				if !slices.Equal(paths, fixturePackages()) {
					t.Errorf("reports = %q, want every fixture package %q", paths, fixturePackages())
				}
				got := violationMetrics(reports, "tested")
				if failing && !containsAll(got, degradedMetrics()) {
					t.Errorf("tested violations = %q, want %q among them", got, degradedMetrics())
				}
				if !failing && len(got) > 0 {
					t.Errorf("tested violations = %q, want none", got)
				}
			})
			t.Run("text", func(t *testing.T) {
				t.Parallel()

				out, _ := check(t, formatText)
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
				for _, m := range degradedMetrics() {
					got := strings.Contains(out, "::error file=tested::"+m+": ")
					if got != failing {
						t.Errorf("github annotates %s on tested = %v, want %v:\n%s", m, got, failing, out)
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
	if len(reports) != 1 || reports[0].PackagePath != "tested" {
		paths := make([]string, 0, len(reports))
		for i := range reports {
			paths = append(paths, reports[i].PackagePath)
		}
		t.Fatalf("checked %q, want only tested", paths)
	}
	if got := violationMetrics(reports, "tested"); !containsAll(got, degradedMetrics()) {
		t.Errorf("tested violations = %q, want %q among them", got, degradedMetrics())
	}
	if b := reports[0].Baseline; b == nil || len(b.Ref) != 40 {
		t.Errorf("baseline = %+v, want the merge-base commit", b)
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
		if reports := decodeReports(t, out.Bytes()); len(reports) != 2 {
			t.Errorf("checked %d packages, want 2", len(reports))
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
