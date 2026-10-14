package engine

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/report"
)

// gitRepoAt initializes a repository on master in a new temporary
// directory, writes files and commits them tagged base, returning the
// directory.
func gitRepoAt(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "master")
	writeFiles(t, dir, files)
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "--no-verify", "-m", "base")
	gitIn(t, dir, "tag", "base")
	return dir
}

// checkAgainstBase loads the module at dir under the default configuration
// and checks it against the tag base, every package when all is set.
func checkAgainstBase(t *testing.T, dir string, all bool) (*report.Check, string) {
	t.Helper()
	cfg, err := config.Parse(pinnedDefault())
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	tg, err := LoadTarget(dir, TargetOptions{Config: cfg, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	if err != nil {
		t.Fatalf("LoadTarget: %v", err)
	}
	c, failed, err := Check(t.Context(), tg, CheckOptions{Base: "base", All: all})
	if err != nil || len(failed) != 0 {
		t.Fatalf("Check = (%v, %v), want no error; logs:\n%s", failed, err, logs.String())
	}
	return c, logs.String()
}

// noBaselineFindings returns "<package> <metric>" for each finding of c
// that has no base value and is not a diff against the baseline itself:
// the findings the text format renders "(no baseline)".
func noBaselineFindings(c *report.Check) []string {
	var out []string
	for i := range c.Packages {
		r := &c.Packages[i].Report
		for _, f := range slices.Concat(r.Violations, r.Warnings) {
			if f.Base == nil && f.Metric != "changed_func_cognitive_max" {
				out = append(out, r.PackagePath+" "+f.Metric)
			}
		}
	}
	return out
}

// TestCheckImportPathOnlyFile checks a baseline file written before
// package_rows existed: its packages are still matched, by the directory
// its module path gives them, with one warning saying how to rewrite it.
func TestCheckImportPathOnlyFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "baseline.json")
	body := `{"ref": "", "module_path": "example.com/m", "packages": {"example.com/m/big": {"tokens_est": 39000}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	tg := fakeTarget("")
	tg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	c, failed, err := Check(t.Context(), tg, CheckOptions{BaselineFile: path, Packages: []string{"example.com/m/big"}})
	if err != nil || len(failed) != 0 {
		t.Fatalf("Check = (%v, %v), want no error", failed, err)
	}
	if r := findRow(c, "big"); r == nil || r.Baseline == nil {
		t.Errorf("big = %+v, want its row from the file", r)
	}
	if n := strings.Count(logs.String(), "baseline file keys packages by import path only"); n != 1 {
		t.Errorf("logged the import-path warning %d times, want 1:\n%s", n, logs.String())
	}
}

// untestedSource is a package of name with n lines of untested exported
// functions, over has_tests's sloc guard.
func untestedSource(name string, n int) string {
	var b strings.Builder
	b.WriteString("package " + name + "\n\n")
	for i := range n / 4 {
		s := strconv.Itoa(i)
		b.WriteString("// F" + s + " returns " + s + ".\nfunc F" + s + "() int {\n\treturn " + s + "\n}\n")
	}
	return b.String()
}

// TestCheckModulePathChange checks a module whose go.mod path changed since
// the baseline (a /v2 bump): every package still finds its baseline row by
// directory, so no finding reads "(no baseline)", and an untouched package
// without tests does not fail has_tests.
func TestCheckModulePathChange(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	dir := gitRepoAt(t, map[string]string{
		"go.mod":         "module example.com/m\n\ngo 1.22\n",
		"root.go":        "package m\n\nimport \"example.com/m/a\"\n\n// R returns a's value.\nfunc R() int { return a.X }\n",
		"a/a.go":         "package a\n\n// X is a's value.\nvar X = 1\n",
		"untested/u.go":  untestedSource("untested", 160),
		"untested/go.go": "package untested\n\nvar state = map[string]int{}\n\n// Set sets k.\nfunc Set(k string) { state[k]++ }\n",
	})
	writeFiles(t, dir, map[string]string{
		"go.mod":  "module example.com/m/v2\n\ngo 1.22\n",
		"root.go": "package m\n\nimport \"example.com/m/v2/a\"\n\n// R returns a's value.\nfunc R() int { return a.X }\n",
	})
	gitIn(t, dir, "commit", "-q", "-a", "--no-verify", "-m", "v2")

	c, logs := checkAgainstBase(t, dir, true)
	if got := noBaselineFindings(c); len(got) != 0 {
		t.Errorf("findings without a baseline: %q, want none; logs:\n%s", got, logs)
	}
	for i := range c.Packages {
		r := &c.Packages[i].Report
		if r.Baseline == nil {
			t.Errorf("%s has no baseline block, want the row at its directory", r.PackagePath)
		}
		if !*r.Passed {
			t.Errorf("%s failed: %+v", r.PackagePath, r.Violations)
		}
	}
}

// moverSource is a file of package name of about 200 lines whose functions
// repeat one block, so the package holds duplicates.
func moverSource(name string) string {
	var b strings.Builder
	b.WriteString("package " + name + "\n\n")
	for i := range 14 {
		s := strconv.Itoa(i)
		b.WriteString("// Calc" + s + " folds xs.\nfunc Calc" + s + "(xs []int) int {\n\ttotal := 0\n" +
			"\tfor i, x := range xs {\n\t\tif x%2 == 0 {\n\t\t\ttotal += x * i\n\t\t} else {\n\t\t\ttotal -= x\n\t\t}\n" +
			"\t\tif total > 1000 {\n\t\t\ttotal %= 1000\n\t\t}\n\t}\n\treturn total + " + s + "\n}\n\n")
	}
	return b.String()
}

// TestCheckCarvedPackage moves a 200-line file with duplicates out of a
// tested package into a new sub-package by git rename. The new package is
// judged against the package it was carved from: it fails neither has_tests
// nor dup_blocks, its text summary says where its baseline came from, and
// the import from the old package to the new one is not a rise in the old
// package's internal_imports.
func TestCheckCarvedPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	dir := gitRepoAt(t, map[string]string{
		"go.mod":          "module example.com/m\n\ngo 1.22\n",
		"big/keep.go":     "package big\n\n// Keep folds a pair.\nfunc Keep() int { return Calc1([]int{1, 2}) }\n",
		"big/mover.go":    moverSource("big"),
		"big/big_test.go": "package big\n\nimport \"testing\"\n\nfunc TestKeep(t *testing.T) {\n\tif Keep() == Calc2(nil) {\n\t\tt.Fatal(\"equal\")\n\t}\n}\n",
	})
	if err := os.MkdirAll(filepath.Join(dir, "big", "carved"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "mv", "big/mover.go", "big/carved/mover.go")
	writeFiles(t, dir, map[string]string{
		"big/carved/mover.go": moverSource("carved"),
		"big/keep.go": "package big\n\nimport \"example.com/m/big/carved\"\n\n" +
			"// Keep folds a pair.\nfunc Keep() int { return carved.Calc1([]int{1, 2}) }\n",
		// The test reaches carved only through Keep, so no test references
		// carved's exports and it has no tests of its own at head.
		"big/big_test.go": "package big\n\nimport \"testing\"\n\n" +
			"func TestKeep(t *testing.T) {\n\tif Keep() == 0 {\n\t\tt.Fatal(\"zero\")\n\t}\n}\n",
	})
	gitIn(t, dir, "commit", "-q", "-a", "--no-verify", "-m", "carve")

	c, logs := checkAgainstBase(t, dir, false)
	carved, big := findRow(c, "big/carved"), findRow(c, "big")
	if carved == nil || big == nil {
		t.Fatalf("checked %d packages, want big and big/carved; logs:\n%s", len(c.Packages), logs)
	}
	if !slices.Equal(carved.BaselineFrom, []string{"big"}) || carved.Baseline == nil {
		t.Errorf("big/carved baseline_from = %q (baseline %v), want seeded from big", carved.BaselineFrom, carved.Baseline != nil)
	}
	if carved.Metrics.HasTests || carved.Metrics.DupBlocks == 0 {
		t.Errorf("big/carved has_tests %v, dup_blocks %d; the fixture wants no tests and duplicates at head",
			carved.Metrics.HasTests, carved.Metrics.DupBlocks)
	}
	for _, f := range carved.Violations {
		if f.Metric == "has_tests" || f.Metric == "dup_blocks" {
			t.Errorf("big/carved violation %s %s, want none on a moved file", f.Metric, f.Limit)
		}
	}
	if got := noBaselineFindings(c); len(got) != 0 {
		t.Errorf("findings without a baseline: %q, want none", got)
	}
	// big imports what was carved out of it: its baseline counts that edge.
	if big.Metrics.InternalImports != 1 || big.Baseline == nil || big.Baseline.Metrics.InternalImports != 1 {
		t.Errorf("big internal_imports head %d, baseline %+v, want 1 and 1", big.Metrics.InternalImports, big.Baseline)
	}
	for _, f := range slices.Concat(big.Violations, big.Warnings) {
		if f.Metric == "internal_imports" {
			t.Errorf("big finding on internal_imports: %+v", f)
		}
	}
	if big.BaselineFrom != nil {
		t.Errorf("big baseline_from = %q, want none: it has its own row", big.BaselineFrom)
	}

	var text bytes.Buffer
	if err := report.WriteCheckText(&text, c); err != nil {
		t.Fatal(err)
	}
	if want := "baseline seeded from big (rename)"; !strings.Contains(text.String(), want) {
		t.Errorf("text output lacks %q:\n%s", want, text.String())
	}
}

// TestCheckCarvedImporters carves big/carved out of big and checks the
// packages that import it at head. user imported big at the baseline and
// imports both big and big/carved at head: the new edge only follows code
// to where it now lives, so user's baseline internal_imports counts it.
// swap imported big and imports only big/carved at head, which is no rise
// to exempt. fresh imports big/carved without ever having imported big,
// which still counts.
func TestCheckCarvedImporters(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	useBig := func(pkg string) string {
		return "package " + pkg + "\n\nimport \"example.com/m/big\"\n\n// Use calls big.\nfunc Use() int { return big.Keep() }\n"
	}
	dir := gitRepoAt(t, map[string]string{
		"go.mod":       "module example.com/m\n\ngo 1.22\n",
		"big/keep.go":  "package big\n\n// Keep folds a pair.\nfunc Keep() int { return Calc1([]int{1, 2}) }\n",
		"big/mover.go": moverSource("big"),
		"user/user.go": useBig("user"),
		"swap/swap.go": useBig("swap"),
		"fresh/new.go": "package fresh\n\n// Use returns one.\nfunc Use() int { return 1 }\n",
	})
	if err := os.MkdirAll(filepath.Join(dir, "big", "carved"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "mv", "big/mover.go", "big/carved/mover.go")
	useCarved := func(pkg string) string {
		return "package " + pkg + "\n\nimport \"example.com/m/big/carved\"\n\n" +
			"// Use calls carved.\nfunc Use() int { return carved.Calc2([]int{3}) }\n"
	}
	writeFiles(t, dir, map[string]string{
		"big/carved/mover.go": moverSource("carved"),
		"big/keep.go": "package big\n\nimport \"example.com/m/big/carved\"\n\n" +
			"// Keep folds a pair.\nfunc Keep() int { return carved.Calc1([]int{1, 2}) }\n",
		"user/user.go": "package user\n\nimport (\n\t\"example.com/m/big\"\n\t\"example.com/m/big/carved\"\n)\n\n" +
			"// Use calls big and carved.\nfunc Use() int { return big.Keep() + carved.Calc2([]int{3}) }\n",
		"swap/swap.go": useCarved("swap"),
		"fresh/new.go": useCarved("fresh"),
	})
	gitIn(t, dir, "commit", "-q", "-a", "--no-verify", "-m", "carve")

	c, logs := checkAgainstBase(t, dir, true)
	for _, tt := range []struct {
		pkg        string
		head, base int
	}{
		{pkg: "user", head: 2, base: 2},
		{pkg: "swap", head: 1, base: 1},
		{pkg: "fresh", head: 1, base: 0},
		{pkg: "big", head: 1, base: 1},
	} {
		r := findRow(c, tt.pkg)
		if r == nil || r.Baseline == nil {
			t.Fatalf("no row with a baseline for %s; logs:\n%s", tt.pkg, logs)
		}
		if r.Metrics.InternalImports != tt.head || r.Baseline.Metrics.InternalImports != tt.base {
			t.Errorf("%s internal_imports head %d, baseline %d, want %d and %d",
				tt.pkg, r.Metrics.InternalImports, r.Baseline.Metrics.InternalImports, tt.head, tt.base)
		}
	}
}
