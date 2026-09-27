package golang

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/metrics"
)

var _ metrics.CoverageMeasurer = (*Extractor)(nil)

func TestParseCoverage(t *testing.T) {
	t.Parallel()

	const stdout = "--- FAIL: TestF (0.00s)\n" +
		"    f_test.go:5: boom\n" +
		"FAIL\n" +
		"coverage: 0.0% of statements\n" +
		"FAIL\tm/fail\t0.356s\n" +
		"FAIL\tm/bad [build failed]\n" +
		"FAIL\tm/vet [build failed]\n" +
		"FAIL\tm/link [build failed]\n" +
		"ok  \tm/good\t0.356s\tcoverage: 81.2% of statements\n" +
		"ok  \tm/nostmt\t0.345s\tcoverage: [no statements]\n" +
		"ok  \tm/zero\t0.345s\tcoverage: 0.0% of statements\n" +
		"ok  \tm/notests\t0.1s\tcoverage: 50.0% of statements [no tests to run]\n" +
		"\tm/untested\t\tcoverage: 0.0% of statements\n" +
		"?   \tm/old\t[no test files]\n" +
		"ok  \tm/other\t0.1s\tcoverage: 10.0% of statements\n" +
		"FAIL\n"
	const stderr = "# m/bad [m/bad.test]\n" +
		"bad/b_test.go:5:28: undefined: nope\n" +
		"# m/vet\n" +
		"# [m/vet]\n" +
		"vet/v_test.go:8:40: fmt.Printf format %d has arg \"x\" of wrong type string\n" +
		"# m/link.test\n" +
		"m/link.TestF: relocation target m/link.gone not defined\n"
	pkgs := []string{"m/fail", "m/bad", "m/vet", "m/link", "m/good", "m/nostmt", "m/zero", "m/notests", "m/untested", "m/old", "m/missing"}

	got := parseCoverage([]byte(stdout), []byte(stderr), pkgs, "")

	tests := []struct {
		pkg    string
		pct    *float64
		reason string
	}{
		{pkg: "m/fail", reason: "tests failed: TestF"},
		{pkg: "m/bad", reason: "test build failed: bad/b_test.go:5:28: undefined: nope"},
		{pkg: "m/vet", reason: "test build failed: vet/v_test.go:8:40: fmt.Printf format %d has arg \"x\" of wrong type string"},
		{pkg: "m/link", reason: "test build failed: m/link.TestF: relocation target m/link.gone not defined"},
		{pkg: "m/good", pct: new(81.2)},
		{pkg: "m/nostmt"},
		{pkg: "m/zero", pct: new(0.0)},
		{pkg: "m/notests", pct: new(50.0)},
		{pkg: "m/untested"},
		{pkg: "m/old"},
		{pkg: "m/missing", reason: "go test reported no result: bad/b_test.go:5:28: undefined: nope"},
	}
	for _, tc := range tests {
		t.Run(tc.pkg, func(t *testing.T) {
			t.Parallel()
			c, ok := got[tc.pkg]
			if !ok {
				t.Fatalf("no entry for %s", tc.pkg)
			}
			switch {
			case tc.pct == nil && c.Pct != nil:
				t.Errorf("Pct = %v, want nil", *c.Pct)
			case tc.pct != nil && c.Pct == nil:
				t.Errorf("Pct = nil, want %v", *tc.pct)
			case tc.pct != nil && *c.Pct != *tc.pct:
				t.Errorf("Pct = %v, want %v", *c.Pct, *tc.pct)
			}
			if c.Reason != tc.reason {
				t.Errorf("Reason = %q, want %q", c.Reason, tc.reason)
			}
		})
	}
	if _, ok := got["m/other"]; ok {
		t.Error("a package outside pkgs got an entry")
	}
	if len(got) != len(pkgs) {
		t.Errorf("got %d entries, want %d", len(got), len(pkgs))
	}
}

func TestParseCoverageStopped(t *testing.T) {
	t.Parallel()

	stdout := "ok  \tm/a\t0.1s\tcoverage: 40.0% of statements\n"
	got := parseCoverage([]byte(stdout), nil, []string{"m/a", "m/b"}, "go test did not finish: context deadline exceeded")
	if c := got["m/a"]; c.Pct == nil || *c.Pct != 40 {
		t.Errorf("m/a = %+v, want 40%%", c)
	}
	if c := got["m/b"]; c.Pct != nil || c.Reason != "go test did not finish: context deadline exceeded" {
		t.Errorf("m/b = %+v, want nil with the stop reason", c)
	}
}

func TestCoverageFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs go test")
	}
	t.Parallel()

	root := fixtureRoot(t)
	mod := &metrics.ModuleContext{Root: root, ModulePath: "example.com/fixture"}
	pkgs := []string{"example.com/fixture/tested", "example.com/fixture/trivial"}
	got, err := New().Coverage(t.Context(), mod, pkgs)
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	tested := got["example.com/fixture/tested"]
	if tested.Pct == nil || *tested.Pct < 0 || *tested.Pct > 100 {
		t.Errorf("tested = %+v, want a percentage in [0, 100]", tested)
	}
	if trivial := got["example.com/fixture/trivial"]; trivial.Pct != nil || trivial.Reason != "" {
		t.Errorf("trivial, which has no test files, = %+v, want nil with no reason", trivial)
	}
}

func TestCoverageBuildFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs go test")
	}
	t.Parallel()

	root := t.TempDir()
	files := map[string]string{
		"go.mod":            "module example.com/cov\n\ngo 1.27\n",
		"good/good.go":      "package good\n\n// F returns 1.\nfunc F() int { return 1 }\n",
		"good/good_test.go": "package good\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) { F() }\n",
		"zero/zero.go":      "package zero\n\n// F returns 1.\nfunc F() int { return 1 }\n",
		"zero/zero_test.go": "package zero\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n",
		"bad/bad.go":        "package bad\n\n// F returns 1.\nfunc F() int { return 1 }\n",
		"bad/bad_test.go":   "package bad\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) { nope() }\n",
	}
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mod := &metrics.ModuleContext{Root: root, ModulePath: "example.com/cov"}
	got, err := New().Coverage(t.Context(), mod, []string{"example.com/cov/good", "example.com/cov/zero", "example.com/cov/bad"})
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	if c := got["example.com/cov/good"]; c.Pct == nil || *c.Pct != 100 {
		t.Errorf("good = %+v, want 100%%", c)
	}
	if c := got["example.com/cov/zero"]; c.Pct == nil || *c.Pct != 0 {
		t.Errorf("zero, whose tests pass and cover nothing, = %+v, want 0", c)
	}
	bad := got["example.com/cov/bad"]
	if bad.Pct != nil || !strings.Contains(bad.Reason, "test build failed") || !strings.Contains(bad.Reason, "undefined: nope") {
		t.Errorf("bad = %+v, want nil with the build error as reason", bad)
	}
}

func TestCoverageTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs go test")
	}
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	mod := &metrics.ModuleContext{Root: fixtureRoot(t), ModulePath: "example.com/fixture"}
	got, err := New().Coverage(ctx, mod, []string{"example.com/fixture/tested"})
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	c := got["example.com/fixture/tested"]
	if c.Pct != nil || !strings.Contains(c.Reason, "did not finish") {
		t.Errorf("tested = %+v, want nil with a stop reason", c)
	}
}
