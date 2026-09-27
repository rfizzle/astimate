package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/report"
)

// assessJSON runs assess --json with args and decodes the report.
func assessJSON(t *testing.T, args ...string) (report.Report, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	args = append([]string{"assess", "--json"}, args...)
	if got := run(args, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, stderr.String())
	}
	var r report.Report
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("decoding report: %v\n%s", err, stdout.String())
	}
	return r, stderr.String()
}

// unspecifiedDriver returns the unspecified driver's detail, or "".
func unspecifiedDriver(r *report.Report) string {
	for _, d := range r.Rebuild.Drivers {
		if d.Term == "unspecified" {
			return d.Detail
		}
	}
	return ""
}

func TestAssessCoverage(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs go test")
	}
	t.Parallel()

	t.Run("tested reports a percentage", func(t *testing.T) {
		t.Parallel()
		r, _ := assessJSON(t, "--coverage", filepath.Join(fixtureDir, "tested"))
		if c := r.Metrics.CoveragePct; c == nil || *c < 0 || *c > 100 {
			t.Errorf("coverage_pct = %v, want a value in [0, 100]", c)
		}
	})
	t.Run("without the flag coverage is null", func(t *testing.T) {
		t.Parallel()
		r, _ := assessJSON(t, filepath.Join(fixtureDir, "tested"))
		if r.Metrics.CoveragePct != nil {
			t.Errorf("coverage_pct = %v, want null without --coverage", *r.Metrics.CoveragePct)
		}
	})
	t.Run("no test files is null without a warning", func(t *testing.T) {
		t.Parallel()
		r, stderr := assessJSON(t, "--coverage", filepath.Join(fixtureDir, "trivial"))
		if r.Metrics.CoveragePct != nil {
			t.Errorf("coverage_pct = %v, want null for a package without tests", *r.Metrics.CoveragePct)
		}
		if strings.Contains(stderr, "coverage") {
			t.Errorf("stderr = %q, want no coverage warning", stderr)
		}
	})
}

// writeCoverModule writes a module with one package whose tests pass and
// cover nothing (zero) and one whose test file type-checks, so extraction
// succeeds, but does not link: a linkname to a missing symbol (bad). It
// returns the root.
func writeCoverModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":            "module example.com/cov\n\ngo 1.27\n",
		"zero/zero.go":      "package zero\n\n// F returns 1.\nfunc F() int { return 1 }\n",
		"zero/zero_test.go": "package zero\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n",
		"bad/bad.go":        "package bad\n\n// F returns 1.\nfunc F() int { return 1 }\n",
		"bad/bad_test.go":   "package bad\n\nimport (\n\t\"testing\"\n\t_ \"unsafe\"\n)\n\n//go:linkname nope example.com/cov/bad.doesNotExist\nfunc nope()\n\nfunc TestF(t *testing.T) { nope() }\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", rel, err)
		}
	}
	return root
}

func TestAssessCoverageBuildFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs go test")
	}
	t.Parallel()

	root := writeCoverModule(t)
	t.Run("build failure is null with a warning", func(t *testing.T) {
		t.Parallel()
		r, stderr := assessJSON(t, "--coverage", filepath.Join(root, "bad"))
		if r.Metrics.CoveragePct != nil {
			t.Errorf("coverage_pct = %v, want null when the tests do not build", *r.Metrics.CoveragePct)
		}
		if n := strings.Count(stderr, "coverage not measured"); n != 1 || !strings.Contains(stderr, "doesNotExist not defined") {
			t.Errorf("stderr = %q, want one warning naming the build error", stderr)
		}
		if r.Metrics.UntestedExports != 1 || r.Metrics.TestFiles != 1 {
			t.Errorf("metrics = %+v, want the package's other metrics unaffected", r.Metrics)
		}
		if d := unspecifiedDriver(&r); strings.Contains(d, "coverage_pct") {
			t.Errorf("unspecified driver = %q, want the unscaled step penalty", d)
		}
	})
	t.Run("passing tests covering nothing report 0", func(t *testing.T) {
		t.Parallel()
		r, _ := assessJSON(t, "--coverage", filepath.Join(root, "zero"))
		if c := r.Metrics.CoveragePct; c == nil || *c != 0 {
			t.Errorf("coverage_pct = %v, want 0", c)
		}
		if d := unspecifiedDriver(&r); !strings.Contains(d, "coverage_pct=0") {
			t.Errorf("unspecified driver = %q, want coverage_pct=0 recorded", d)
		}
	})
}

func TestRankAndCheckCoverage(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs go test")
	}
	t.Parallel()

	t.Run("rank", func(t *testing.T) {
		t.Parallel()
		var stdout, stderr bytes.Buffer
		args := []string{"rank", "--json", "--coverage", fixtureDir}
		if got := run(args, &stdout, &stderr); got != exitOK {
			t.Fatalf("run(%q) = %d, want %d; stderr = %q", args, got, exitOK, stderr.String())
		}
		if strings.Contains(stderr.String(), "coverage not measured") {
			t.Errorf("stderr = %q, want no coverage warning for the fixture", stderr.String())
		}
	})
	t.Run("check measures head only", func(t *testing.T) {
		t.Parallel()
		base := filepath.Join(t.TempDir(), "base.json")
		var stdout, stderr bytes.Buffer
		if got := run([]string{"baseline", "write", fixtureDir, "--out", base}, &stdout, &stderr); got != exitOK {
			t.Fatalf("baseline write = %d; stderr = %q", got, stderr.String())
		}
		stdout.Reset()
		stderr.Reset()
		args := []string{"check", fixtureDir, "--baseline", base, "--all", "--coverage", "--format", "json"}
		if got := run(args, &stdout, &stderr); got != exitOK && got != exitGateFailed {
			t.Fatalf("run(%q) = %d; stderr = %q", args, got, stderr.String())
		}
		var reports []report.Report
		if err := json.Unmarshal(stdout.Bytes(), &reports); err != nil {
			t.Fatalf("decoding check: %v\n%s", err, stdout.String())
		}
		found := false
		for i := range reports {
			r := &reports[i]
			if r.PackagePath != "tested" {
				continue
			}
			found = true
			if c := r.Metrics.CoveragePct; c == nil || *c < 0 || *c > 100 {
				t.Errorf("head coverage_pct = %v, want a value in [0, 100]", c)
			}
			if r.Baseline == nil || r.Baseline.Metrics.CoveragePct != nil {
				t.Errorf("baseline = %+v, want present with null coverage_pct", r.Baseline)
			}
		}
		if !found {
			t.Errorf("check reported no tested package:\n%s", stdout.String())
		}
	})
}

func TestCoverageTimeoutFlag(t *testing.T) {
	t.Parallel()

	for _, cmd := range []string{"assess", "rank", "check"} {
		t.Run(cmd, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			args := []string{cmd, "--coverage", "--coverage-timeout", "0s", filepath.Join(fixtureDir, "tested")}
			if got := run(args, &stdout, &stderr); got != exitUsage {
				t.Errorf("run(%q) = %d, want %d; stderr = %q", args, got, exitUsage, stderr.String())
			}
			if !strings.Contains(stderr.String(), "--coverage-timeout must be positive") {
				t.Errorf("stderr = %q, want the timeout error", stderr.String())
			}
		})
	}
}
