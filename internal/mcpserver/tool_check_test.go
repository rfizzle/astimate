package mcpserver

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

// Fixture modules relative to this package's directory; see testdata/go.
const (
	fixtureDir  = "../../testdata/go/fixture"
	degradedDir = "../../testdata/go/fixture-degraded"
	extmodDir   = "../../testdata/go/extmod"
)

// degradedMetrics are the rules the degraded fixture's tested package
// breaks by adding a duplicate function, an untested export, a global and
// one function of cognitive complexity 40.
func degradedMetrics() []string {
	return []string{"changed_func_cognitive_max", "dup_blocks", "globals", "untested_exports"}
}

// defaultConfig returns the embedded default configuration.
func defaultConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, _, err := config.Resolve("")
	if err != nil {
		t.Fatalf("resolving default config: %v", err)
	}
	return cfg
}

// uncalibratedConfig returns the embedded default with its config_version
// replaced by one of a thresholds-only calibration, so its rebuild estimate
// counts as uncalibrated (score.Calibrated) and the text leaves it out.
func uncalibratedConfig(t *testing.T) *config.Config {
	t.Helper()
	lines := strings.Split(string(config.Default()), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "config_version: ") {
			lines[i] = "config_version: thresholds-2026-09-28"
		}
	}
	cfg, err := config.Parse([]byte(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatalf("parsing the uncalibrated config: %v", err)
	}
	if strings.HasPrefix(cfg.Version, "rebuild-") {
		t.Fatalf("uncalibrated config version = %q, want no rebuild- version", cfg.Version)
	}
	return cfg
}

// workspace copies the fixture module src to <tmp>/mod, writes the pristine
// fixture's baseline to <tmp>/baseline.json, and returns tmp with symlinks
// resolved. The copy is outside any git repository, so only the baseline
// file can serve as the baseline.
func workspace(t *testing.T, src string) string {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// The fixtures replace example.com/extmod with ../extmod.
	for dst, from := range map[string]string{"mod": src, "extmod": extmodDir} {
		if err := os.CopyFS(filepath.Join(tmp, dst), os.DirFS(from)); err != nil {
			t.Fatalf("copying %s: %v", from, err)
		}
	}
	tg, err := engine.LoadTarget(fixtureDir, engine.TargetOptions{Config: defaultConfig(t)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.WriteBaseline(t.Context(), tg, filepath.Join(tmp, "baseline.json")); err != nil {
		t.Fatalf("writing the fixture baseline: %v", err)
	}
	return tmp
}

// callCheck calls check_package with in and returns the result.
func callCheck(t *testing.T, cs *mcp.ClientSession, in map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: checkToolName, Arguments: in})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	return res
}

// resultText joins the text content of res.
func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// decodeStrict decodes the structured content of res as a T, rejecting
// unknown fields.
func decodeStrict[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("encoding structured content: %v", err)
	}
	var v T
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decoding structured content as a %T: %v\n%s", v, err, data)
	}
	return v
}

// decodeReport decodes the structured content of res as a report,
// rejecting unknown fields.
func decodeReport(t *testing.T, res *mcp.CallToolResult) report.Report {
	t.Helper()
	return decodeStrict[report.Report](t, res)
}

// decodeCheck decodes the structured content of res as a check_package
// result, rejecting unknown fields.
func decodeCheck(t *testing.T, res *mcp.CallToolResult) CheckResult {
	t.Helper()
	return decodeStrict[CheckResult](t, res)
}

func TestCheckPackageTool(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	tests := []struct {
		name       string
		src        string
		calibrated bool
		wantPassed bool
		wantMetric []string
		wantText   string
	}{
		{name: "degraded uncalibrated", src: degradedDir, wantPassed: false, wantMetric: degradedMetrics(), wantText: "FAILED"},
		{name: "unchanged uncalibrated", src: fixtureDir, wantPassed: true, wantText: "PASSED"},
		{name: "degraded", src: degradedDir, calibrated: true, wantPassed: false, wantMetric: degradedMetrics(), wantText: "FAILED"},
		{name: "unchanged", src: fixtureDir, calibrated: true, wantPassed: true, wantText: "PASSED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ws := workspace(t, tt.src)
			cfg := defaultConfig(t)
			if !tt.calibrated {
				cfg = uncalibratedConfig(t)
			}
			cs := newTestClient(t, Options{Config: cfg, WorkDir: ws, Version: "test"})
			res := callCheck(t, cs, map[string]any{"path": "mod/tested", "baseline_file": "baseline.json"})
			text := resultText(res)
			if res.IsError {
				t.Fatalf("IsError = true, want a gate result; text:\n%s", text)
			}
			cr := decodeCheck(t, res)
			r := cr.Report
			if cr.Module == nil || cr.Module.PackagePath != metrics.ModuleRowID || cr.Module.Passed == nil {
				t.Errorf("module block = %+v, want the gated module row", cr.Module)
			}
			if r.Passed == nil || *r.Passed != tt.wantPassed {
				t.Fatalf("passed = %v, want %v; text:\n%s", r.Passed, tt.wantPassed, text)
			}
			if r.PackagePath != "tested" || r.Baseline == nil {
				t.Errorf("report = (%q, baseline %v), want tested with a baseline", r.PackagePath, r.Baseline)
			}
			got := make([]string, 0, len(r.Violations))
			for _, v := range r.Violations {
				got = append(got, v.Metric)
			}
			slices.Sort(got)
			got = slices.Compact(got)
			// The duplicate function also moves duplication_pct, so the
			// three rules the fixture breaks on purpose are checked as a
			// subset, as the CLI's fixture tests do.
			missing := slices.DeleteFunc(slices.Clone(tt.wantMetric), func(m string) bool {
				return slices.Contains(got, m)
			})
			if len(missing) > 0 || (len(tt.wantMetric) == 0 && len(got) > 0) {
				t.Errorf("violation metrics = %v, want %v among them and none when passing", got, tt.wantMetric)
			}
			if !strings.HasPrefix(text, tt.wantText) {
				t.Errorf("text = %q, want it to start with %q", text, tt.wantText)
			}
			for _, m := range tt.wantMetric {
				if !strings.Contains(text, m+": ") {
					t.Errorf("text does not name violation %s:\n%s", m, text)
				}
			}
			// The package's summary line carries agent passes and tier
			// only when the rebuild estimate is calibrated; the structured
			// report carries them either way.
			summary := ""
			for l := range strings.SplitSeq(text, "\n") {
				if strings.HasPrefix(l, "tested: ") {
					summary = l
				}
			}
			if got := strings.Contains(summary, " passes (") && strings.Contains(summary, " passes from baseline"); got != tt.calibrated {
				t.Errorf("summary line %q carries the estimate = %v, want %v", summary, got, tt.calibrated)
			}
			if !tt.calibrated {
				for _, m := range []string{" passes", "ONE_PASS", "FEW_PASSES", "PARTITION"} {
					if strings.Contains(text, m) {
						t.Errorf("text carries %q from the uncalibrated estimate:\n%s", m, text)
					}
				}
			}
			if r.Rebuild.Tier == "" || r.Rebuild.Calibrated != tt.calibrated {
				t.Errorf("rebuild = tier %q calibrated %v, want a tier and calibrated %v", r.Rebuild.Tier, r.Rebuild.Calibrated, tt.calibrated)
			}
		})
	}
}

func TestCheckPackageToolErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	ws := workspace(t, fixtureDir)
	outside := t.TempDir()
	link := filepath.Join(ws, "mod", "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig(t)
	inTested := Options{Config: cfg, WorkDir: filepath.Join(ws, "mod", "tested")}

	tests := []struct {
		name     string
		opts     Options
		in       map[string]any
		wantText string
	}{
		{name: "parent path refused", opts: inTested,
			in: map[string]any{"path": "../hub"}, wantText: "outside the server's working directory"},
		{name: "absolute path refused", opts: inTested,
			in: map[string]any{"path": outside}, wantText: "outside the server's working directory"},
		{name: "symlink out refused", opts: Options{Config: cfg, WorkDir: ws},
			in: map[string]any{"path": "mod/escape"}, wantText: "outside the server's working directory"},
		{name: "baseline file outside refused", opts: inTested,
			in: map[string]any{"path": ".", "baseline_file": "../../baseline.json"}, wantText: "baseline_file"},
		{name: "missing path", opts: Options{Config: cfg, WorkDir: ws},
			in: map[string]any{"path": "mod/nope"}, wantText: "mod/nope"},
		{name: "base and baseline file", opts: Options{Config: cfg, WorkDir: ws},
			in:       map[string]any{"path": "mod/tested", "base": "main", "baseline_file": "baseline.json"},
			wantText: "mutually exclusive"},
		{name: "no baseline", opts: Options{Config: cfg, WorkDir: ws},
			in: map[string]any{"path": "mod/tested"}, wantText: "pass base (a git ref) or baseline_file"},
		{name: "staged outside a repository", opts: Options{Config: cfg, WorkDir: ws},
			in:       map[string]any{"path": "mod/tested", "baseline_file": "baseline.json", "staged": true},
			wantText: "not in a git repository"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := callCheck(t, newTestClient(t, tt.opts), tt.in)
			text := resultText(res)
			if !res.IsError {
				t.Fatalf("IsError = false, want true; text:\n%s", text)
			}
			if !strings.Contains(text, tt.wantText) {
				t.Errorf("text = %q, want it to contain %q", text, tt.wantText)
			}
			if strings.Contains(text, "--base") {
				t.Errorf("text = %q, want no CLI flag hint", text)
			}
			data, err := json.Marshal(res.StructuredContent)
			if err != nil || !bytes.Contains(data, []byte(`"error"`)) {
				t.Errorf("structured content = %s, want an error object", data)
			}
		})
	}

	t.Run("allow any path", func(t *testing.T) {
		t.Parallel()

		o := inTested
		o.AllowAnyPath = true
		res := callCheck(t, newTestClient(t, o), map[string]any{"path": "../hub", "baseline_file": "../../baseline.json"})
		if res.IsError {
			t.Fatalf("IsError = true with --allow-any-path; text:\n%s", resultText(res))
		}
		if r := decodeCheck(t, res); r.PackagePath != "hub" || r.Passed == nil || !*r.Passed {
			t.Errorf("report = (%q, passed %v), want hub passing", r.PackagePath, r.Passed)
		}
	})
}

// gitIn runs git with args in dir, isolated from the user's and the
// system's configuration and from any repository a git hook points at.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
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

// crossPackageConfig returns the default configuration with exactly one
// rule on dup_blocks_cross_pkg: max_delta 0 with ratchet_from_zero.
func crossPackageConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	zero := 0.0
	cfg.Thresholds = slices.DeleteFunc(cfg.Thresholds, func(r gate.Threshold) bool { return metrics.ModuleWide(r.Metric) })
	cfg.Thresholds = append(cfg.Thresholds, gate.Threshold{
		Metric: "dup_blocks_cross_pkg", Kind: gate.Density, MaxDelta: &zero, RatchetFromZero: true,
	})
	return cfg
}

// copiedSumOrders is an unexported copy of dupes.SumOrders under other
// names, written into package b: code the fixture repeats only within
// dupes, now shared with a second package, so new cross-package blocks. b
// already nests as deep and is as complex, so its own rules still pass.
const copiedSumOrders = `package b

func sumLines(lines []int, least int) (int, string) {
	count := 0
	missed := 0
	for k, l := range lines {
		if k >= 40 {
			break
		}
		if l < least {
			missed++
			continue
		}
		count += l * 5
	}
	if missed > 3 {
		return count, "lines-partial"
	}
	return count, "lines"
}
`

// TestCheckPackageModuleRow checks the fixture, committed on master, with a
// rule on dup_blocks_cross_pkg: a copy of another package's code made in
// the checked package fails check_package through the module block, while
// the package's own report has no violation; without the copy it passes.
func TestCheckPackageModuleRow(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	for _, copied := range []bool{true, false} {
		name := map[bool]string{true: "cross-package copy", false: "no copy"}[copied]
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repo, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			// The repository holds the module the fixture's replace
			// directive points at, so the baseline worktree resolves it.
			for dst, from := range map[string]string{"fixture": fixtureDir, "extmod": extmodDir} {
				if err := os.CopyFS(filepath.Join(repo, dst), os.DirFS(from)); err != nil {
					t.Fatalf("copying %s: %v", from, err)
				}
			}
			gitIn(t, repo, "init", "-q", "-b", "master")
			gitIn(t, repo, "add", "-A")
			gitIn(t, repo, "commit", "-q", "--no-verify", "-m", "pristine fixture")
			if copied {
				path := filepath.Join(repo, "fixture", "b", "copy.go")
				if err := os.WriteFile(path, []byte(copiedSumOrders), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			cs := newTestClient(t, Options{Config: crossPackageConfig(t), WorkDir: repo, Version: "test"})
			res := callCheck(t, cs, map[string]any{"path": "fixture/b", "base": "master"})
			text := resultText(res)
			if res.IsError {
				t.Fatalf("IsError = true, want a gate result; text:\n%s", text)
			}
			cr := decodeCheck(t, res)
			if cr.PackagePath != "b" || len(cr.Violations) != 0 {
				t.Errorf("package report = %s with violations %+v, want b with none", cr.PackagePath, cr.Violations)
			}
			m := cr.Module
			if m == nil || m.PackagePath != metrics.ModuleRowID || m.Passed == nil {
				t.Fatalf("module block = %+v, want the gated module row", m)
			}
			var got []string
			for _, v := range m.Violations {
				got = append(got, v.Metric)
			}
			var wantViolations []string
			if copied {
				wantViolations = []string{"dup_blocks_cross_pkg"}
			}
			if !slices.Equal(got, wantViolations) || *m.Passed != !copied {
				t.Errorf("module violations = %v, passed %v; want %v", got, *m.Passed, wantViolations)
			}
			if cr.Passed == nil || *cr.Passed != !copied {
				t.Fatalf("passed = %v, want %v; text:\n%s", cr.Passed, !copied, text)
			}
			wantText := map[bool]string{true: "FAILED", false: "PASSED"}[copied]
			if !strings.HasPrefix(text, wantText) {
				t.Errorf("text = %q, want it to start with %q", text, wantText)
			}
			if copied && !strings.Contains(text, "\n  "+metrics.ModuleRowID+"\n    dup_blocks_cross_pkg: 1 -> ") {
				t.Errorf("text does not list the module violation under %s:\n%s", metrics.ModuleRowID, text)
			}
		})
	}
}

// sharedLabel is a function written into both hub and hidden: a
// cross-package block between those two packages only.
const sharedLabel = `

func label(n int, unit string) string {
	switch {
	case n < 0:
		return "negative " + unit
	case n == 0:
		return "no " + unit
	case n < 10:
		return "a few " + unit
	case n < 100:
		return "many " + unit
	default:
		return "countless " + unit
	}
}
`

// TestCheckPackageForeignCopy checks the fixture, committed on master and
// already sharing one block between a and b, after a new copy between hub
// and hidden, with a rule on dup_blocks_cross_pkg: the check of trivial,
// which the copy does not touch, passes with the module row still counting
// the copy, while the check of hub fails on the module row and names hub
// and hidden as the packages sharing the new block.
func TestCheckPackageForeignCopy(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for dst, from := range map[string]string{"fixture": fixtureDir, "extmod": extmodDir} {
		if err := os.CopyFS(filepath.Join(repo, dst), os.DirFS(from)); err != nil {
			t.Fatalf("copying %s: %v", from, err)
		}
	}
	gitIn(t, repo, "init", "-q", "-b", "master")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "--no-verify", "-m", "pristine fixture")
	for _, pkg := range []string{"hub", "hidden"} {
		path := filepath.Join(repo, "fixture", pkg, "label.go")
		if err := os.WriteFile(path, []byte("package "+pkg+sharedLabel), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cs := newTestClient(t, Options{Config: crossPackageConfig(t), WorkDir: repo, Version: "test"})

	tests := []struct {
		pkg        string
		wantPassed bool
	}{
		{pkg: "trivial", wantPassed: true},
		{pkg: "hub"},
	}
	for _, tt := range tests {
		res := callCheck(t, cs, map[string]any{"path": "fixture/" + tt.pkg, "base": "master"})
		text := resultText(res)
		if res.IsError {
			t.Fatalf("%s: IsError = true, want a gate result; text:\n%s", tt.pkg, text)
		}
		m := decodeCheck(t, res).Module
		if m == nil || m.Passed == nil {
			t.Fatalf("%s: module block = %+v, want the gated module row", tt.pkg, m)
		}
		if n := m.Metrics.DupBlocksCrossPkg; n == nil || *n != 2 {
			t.Errorf("%s: module dup_blocks_cross_pkg = %v, want the full count 2", tt.pkg, n)
		}
		var got []string
		for _, v := range m.Violations {
			got = append(got, v.Metric)
		}
		if tt.wantPassed {
			if len(got) != 0 || !*m.Passed || !strings.HasPrefix(text, "PASSED") {
				t.Errorf("%s: module violations = %v, passed %v, want none; text:\n%s", tt.pkg, got, *m.Passed, text)
			}
			continue
		}
		if !slices.Equal(got, []string{"dup_blocks_cross_pkg"}) || *m.Passed || !strings.HasPrefix(text, "FAILED") {
			t.Fatalf("%s: module violations = %v, passed %v, want one dup_blocks_cross_pkg; text:\n%s",
				tt.pkg, got, *m.Passed, text)
		}
		const shared = "shared by hidden and hub."
		if !strings.Contains(m.Violations[0].Suggestion, shared) || !strings.Contains(text, shared) {
			t.Errorf("%s: suggestion = %q, want it and the text to say %q; text:\n%s",
				tt.pkg, m.Violations[0].Suggestion, shared, text)
		}
		if !strings.Contains(text, "dup_blocks_cross_pkg: 1 -> 2") {
			t.Errorf("%s: text does not count one new block against the baseline:\n%s", tt.pkg, text)
		}
	}
}

// TestCheckPackageStaged checks the fixture, committed on master, with a
// good change staged and a new global left unstaged in the same file:
// staged: true judges the index and passes, while the working tree fails
// on globals; once the global is staged, staged: true fails too.
func TestCheckPackageStaged(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for dst, from := range map[string]string{"fixture": fixtureDir, "extmod": extmodDir} {
		if err := os.CopyFS(filepath.Join(repo, dst), os.DirFS(from)); err != nil {
			t.Fatalf("copying %s: %v", from, err)
		}
	}
	gitIn(t, repo, "init", "-q", "-b", "master")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "--no-verify", "-m", "pristine fixture")

	src := filepath.Join(repo, "fixture", "tested", "tested.go")
	pristine, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	good := append(slices.Clip(pristine), "\n// Join and Bounded are covered by tested_test.go.\n"...)
	if err := os.WriteFile(src, good, 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", "fixture/tested/tested.go")
	degraded := append(slices.Clip(good), "\nvar calls int\n"...)
	if err := os.WriteFile(src, degraded, 0o600); err != nil {
		t.Fatal(err)
	}

	cs := newTestClient(t, Options{Config: defaultConfig(t), WorkDir: repo, Version: "test"})
	check := func(t *testing.T, staged any, wantPassed bool) {
		t.Helper()
		in := map[string]any{"path": "fixture/tested", "base": "master"}
		if staged != nil {
			in["staged"] = staged
		}
		res := callCheck(t, cs, in)
		text := resultText(res)
		if res.IsError {
			t.Fatalf("IsError = true, want a gate result; text:\n%s", text)
		}
		cr := decodeCheck(t, res)
		if cr.Passed == nil || *cr.Passed != wantPassed {
			t.Fatalf("passed = %v, want %v; text:\n%s", cr.Passed, wantPassed, text)
		}
		hasGlobals := slices.ContainsFunc(cr.Violations, func(v report.Finding) bool { return v.Metric == "globals" })
		if hasGlobals == wantPassed {
			t.Errorf("globals violation = %v, want %v; text:\n%s", hasGlobals, !wantPassed, text)
		}
	}

	// The subtests share one repository and run in order.
	t.Run("unstaged degradation, staged true", func(t *testing.T) { check(t, true, true) })
	t.Run("unstaged degradation, staged false", func(t *testing.T) { check(t, false, false) })
	t.Run("unstaged degradation, staged absent", func(t *testing.T) { check(t, nil, false) })
	gitIn(t, repo, "add", "fixture/tested/tested.go")
	t.Run("staged degradation, staged true", func(t *testing.T) { check(t, true, false) })
}
