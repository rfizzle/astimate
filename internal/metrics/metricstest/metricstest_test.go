package metricstest_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
)

// The failure-detection tests run the suite in a child process: the test
// binary re-executes itself with -test.run selecting TestSuiteSubprocess and
// caseEnv naming the broken setup. The child's failure is asserted from its
// exit status and output, so the parent test stays green.
const (
	caseEnv          = "METRICSTEST_SUBPROCESS_CASE"
	caseGolden       = "golden-mutation"
	caseInvariant    = "invariant-violation"
	caseDetails      = "details-mismatch"
	caseRatio        = "ratio-noise"
	caseModuleShape  = "module-shape"
	caseModuleSum    = "module-sum"
	caseModuleValid  = "module-invalid"
	fakeRoot         = "/fake/module"
	thisPackage      = "github.com/rfizzle/astimate/internal/metrics"
	metricstestPkgID = thisPackage + "/metricstest"
)

// syntheticPackages returns a three-package module: alpha imports beta and
// gamma, beta imports gamma. Fan-out sums to 3 and so does fan-in. One
// duplicate block is shared by alpha and beta, so each counts one
// cross-package block; gamma leaves the count null.
func syntheticPackages() map[string]metrics.RawMetrics {
	coverage := 71.5
	oneCross := 1
	noCgo := false
	// beta's coupling ratios: fan_in 1 and internal_imports 1 give
	// instability 0.5, and |0.5 + 0.5 - 1| is 0.
	half, zero := 0.5, 0.0
	return map[string]metrics.RawMetrics{
		"alpha": {
			Files: 2, SLOC: 120, LargestFileSLOC: 80, TokensEst: 900, TokensEstWithTests: 1300,
			InternalImports: 2, ExternalImports: 1, StdlibImports: 3,
			ExportedSymbols: 5, MaxNesting: 3, CognitiveTotal: 14, CognitiveP90: 6, FuncCount: 7,
			TestFiles: 1, TestFuncs: 4, HasTests: true, UntestedExports: 1,
			CoveragePct:       &coverage, // computed but absent from the golden, so not compared
			DupBlocksCrossPkg: &oneCross,
		},
		"beta": {
			Files: 1, SLOC: 40, LargestFileSLOC: 40, TokensEst: 300, TokensEstWithTests: 300,
			InternalImports: 1, StdlibImports: 1, FanIn: 1,
			ExportedSymbols: 2, Globals: 1, MaxNesting: 1, CognitiveTotal: 2, CognitiveP90: 1, FuncCount: 2,
			DupBlocks: 1, DuplicationPct: 45.5, UntestedExports: 2,
			Instability: &half, Abstractness: &half, MainSequenceDistance: &zero,
			DupBlocksCrossPkg: &oneCross,
		},
		"gamma": {
			Files: 1, SLOC: 10, LargestFileSLOC: 10, TokensEst: 70, TokensEstWithTests: 150,
			FanIn: 2, FanInTests: 1, ExportedSymbols: 1, FuncCount: 1,
			TestFiles: 1, TestFuncs: 1, HasTests: true,
			UsesCgo: &noCgo,
		},
	}
}

// syntheticGoldens are written by hand, independently of syntheticPackages,
// so that a passing run also proves LoadGolden decodes the documented format.
func syntheticGoldens() map[string]string {
	return map[string]string{
		"alpha": `{
  "files": 2,
  "sloc": 120,
  "largest_file_sloc": 80,
  "tokens_est": 900,
  "tokens_est_with_tests": 1300,
  "internal_imports": 2,
  "external_imports": 1,
  "stdlib_imports": 3,
  "fan_in": 0,
  "fan_in_tests": 0,
  "exported_symbols": 5,
  "globals": 0,
  "init_funcs": 0,
  "max_nesting": 3,
  "cognitive_total": 14,
  "cognitive_p90": 6,
  "func_count": 7,
  "dup_blocks": 0,
  "duplication_pct": 0,
  "test_files": 1,
  "test_funcs": 4,
  "has_tests": true,
  "untested_exports": 1
}
`,
		"beta": `{
  "files": 1,
  "sloc": 40,
  "largest_file_sloc": 40,
  "tokens_est": 300,
  "tokens_est_with_tests": 300,
  "internal_imports": 1,
  "external_imports": 0,
  "stdlib_imports": 1,
  "fan_in": 1,
  "fan_in_tests": 0,
  "exported_symbols": 2,
  "globals": 1,
  "init_funcs": 0,
  "max_nesting": 1,
  "cognitive_total": 2,
  "cognitive_p90": 1,
  "func_count": 2,
  "dup_blocks": 1,
  "duplication_pct": 45.5,
  "test_files": 0,
  "test_funcs": 0,
  "has_tests": false,
  "untested_exports": 2
}
`,
		"gamma": `{
  "files": 1,
  "sloc": 10,
  "largest_file_sloc": 10,
  "tokens_est": 70,
  "tokens_est_with_tests": 150,
  "internal_imports": 0,
  "external_imports": 0,
  "stdlib_imports": 0,
  "fan_in": 2,
  "fan_in_tests": 1,
  "exported_symbols": 1,
  "globals": 0,
  "init_funcs": 0,
  "max_nesting": 0,
  "cognitive_total": 0,
  "cognitive_p90": 0,
  "func_count": 1,
  "dup_blocks": 0,
  "duplication_pct": 0,
  "test_files": 1,
  "test_funcs": 1,
  "has_tests": true,
  "untested_exports": 0,
  "uses_cgo": false,
  "generated_files": null
}
`,
	}
}

// writeGoldens writes goldens into a fresh temporary directory and returns it.
func writeGoldens(t *testing.T, goldens map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for pkg, body := range goldens {
		if err := os.WriteFile(filepath.Join(dir, pkg+".json"), []byte(body), 0o644); err != nil {
			t.Fatalf("writing golden %s: %v", pkg, err)
		}
	}
	return dir
}

func syntheticFixture(dir string) metricstest.Fixture {
	return metricstest.Fixture{
		Root:      fakeRoot,
		Packages:  []string{"alpha", "beta", "gamma"},
		GoldenDir: dir,
	}
}

func TestSuitePassesOnFake(t *testing.T) {
	dir := writeGoldens(t, syntheticGoldens())
	ext := metricstest.NewFake("fake", fakeRoot, syntheticPackages())
	metricstest.TestExtractor(t, ext, syntheticFixture(dir))
}

// syntheticDetails names as many untested exports per package as
// syntheticPackages counts.
func syntheticDetails() map[string]metrics.Details {
	return map[string]metrics.Details{
		"alpha": {UntestedExports: []string{"Parse"}},
		"beta":  {UntestedExports: []string{"Close", "Open"}, DupLocations: []string{"beta.go:3-9", "beta.go:12-18"}},
	}
}

func TestSuitePassesOnFakeWithDetails(t *testing.T) {
	dir := writeGoldens(t, syntheticGoldens())
	ext := metricstest.NewFake("fake", fakeRoot, syntheticPackages(), metricstest.WithDetails(syntheticDetails()))
	if _, ok := ext.(metrics.Detailer); !ok {
		t.Fatal("NewFake with WithDetails does not implement metrics.Detailer")
	}
	metricstest.TestExtractor(t, ext, syntheticFixture(dir))
}

// syntheticModuleRow is the module row of syntheticPackages: the one block
// alpha and beta share.
func syntheticModuleRow() metrics.RawMetrics {
	blocks := 1
	return metrics.RawMetrics{DupBlocksCrossPkg: &blocks}
}

// syntheticModuleGolden is the hand-written golden of syntheticModuleRow, in
// the layout the suite writes on update.
const syntheticModuleGolden = `{
  "files": 0,
  "sloc": 0,
  "largest_file_sloc": 0,
  "tokens_est": 0,
  "tokens_est_with_tests": 0,
  "internal_imports": 0,
  "external_imports": 0,
  "stdlib_imports": 0,
  "fan_in": 0,
  "fan_in_tests": 0,
  "exported_symbols": 0,
  "globals": 0,
  "init_funcs": 0,
  "max_nesting": 0,
  "cognitive_total": 0,
  "cognitive_p90": 0,
  "func_count": 0,
  "dup_blocks": 0,
  "duplication_pct": 0,
  "test_files": 0,
  "test_funcs": 0,
  "has_tests": false,
  "untested_exports": 0,
  "dup_blocks_cross_pkg": 1
}
`

// syntheticGoldensWithModule adds the module row's golden to
// syntheticGoldens.
func syntheticGoldensWithModule() map[string]string {
	goldens := syntheticGoldens()
	goldens[metrics.ModuleRowID] = syntheticModuleGolden
	return goldens
}

func TestSuitePassesOnFakeWithModuleRow(t *testing.T) {
	for name, opts := range map[string][]metricstest.FakeOption{
		"row only":         {metricstest.WithModuleRow(syntheticModuleRow())},
		"row and details":  {metricstest.WithModuleRow(syntheticModuleRow()), metricstest.WithDetails(syntheticDetails())},
		"details then row": {metricstest.WithDetails(syntheticDetails()), metricstest.WithModuleRow(syntheticModuleRow())},
	} {
		t.Run(name, func(t *testing.T) {
			dir := writeGoldens(t, syntheticGoldensWithModule())
			ext := metricstest.NewFake("fake", fakeRoot, syntheticPackages(), opts...)
			if _, ok := ext.(metrics.ModuleMetrics); !ok {
				t.Fatal("NewFake with WithModuleRow does not implement metrics.ModuleMetrics")
			}
			if _, ok := ext.(metrics.Detailer); ok != (len(opts) == 2) {
				t.Errorf("NewFake implements metrics.Detailer = %v with %d options", ok, len(opts))
			}
			metricstest.TestExtractor(t, ext, syntheticFixture(dir))
		})
	}
}

func TestFakeWithoutModuleRowIsNotModuleMetrics(t *testing.T) {
	for name, ext := range map[string]metrics.Extractor{
		"plain":        metricstest.NewFake("fake", fakeRoot, syntheticPackages()),
		"details only": metricstest.NewFake("fake", fakeRoot, syntheticPackages(), metricstest.WithDetails(nil)),
	} {
		if _, ok := ext.(metrics.ModuleMetrics); ok {
			t.Errorf("%s: NewFake without WithModuleRow implements metrics.ModuleMetrics", name)
		}
	}
}

// TestUpdateRewritesModuleGolden checks that update mode writes module.json
// in the hand-written layout, and that compare mode then accepts it.
func TestUpdateRewritesModuleGolden(t *testing.T) {
	dir := t.TempDir()
	ext := metricstest.NewFake("fake", fakeRoot, syntheticPackages(), metricstest.WithModuleRow(syntheticModuleRow()))
	fx := syntheticFixture(dir)
	fx.Update = true
	metricstest.TestExtractor(t, ext, fx)

	fx.Update = false
	metricstest.TestExtractor(t, ext, fx)

	got, err := os.ReadFile(filepath.Join(dir, metrics.ModuleRowID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != syntheticModuleGolden {
		t.Errorf("rewritten module golden differs from the hand-written layout:\ngot:\n%s\nwant:\n%s", got, syntheticModuleGolden)
	}
}

func TestFakeWithoutDetailsIsNotDetailer(t *testing.T) {
	if _, ok := metricstest.NewFake("fake", fakeRoot, syntheticPackages()).(metrics.Detailer); ok {
		t.Error("NewFake without WithDetails implements metrics.Detailer; callers could not test their fallback")
	}
}

// TestSuiteSubprocess is the child side of the failure-detection tests. It
// does nothing unless caseEnv is set.
func TestSuiteSubprocess(t *testing.T) {
	pkgs := syntheticPackages()
	goldens := syntheticGoldens()
	fx := syntheticFixture("")
	var opts []metricstest.FakeOption
	switch os.Getenv(caseEnv) {
	case "":
		return
	case caseGolden:
		goldens["beta"] = strings.Replace(goldens["beta"], `"sloc": 40`, `"sloc": 41`, 1)
	case caseInvariant:
		alpha := pkgs["alpha"]
		alpha.FanIn = 1
		pkgs["alpha"] = alpha
	case caseRatio:
		// alpha has internal_imports 2 and fan_in 0, so instability is 1,
		// reported here with float noise; beta's distance disagrees with its
		// other two ratios.
		noisy, off := 0.9999999999999999, 0.25
		alpha := pkgs["alpha"]
		alpha.Instability = &noisy
		pkgs["alpha"] = alpha
		beta := pkgs["beta"]
		beta.MainSequenceDistance = &off
		pkgs["beta"] = beta
	case caseDetails:
		details := syntheticDetails()
		details["beta"] = metrics.Details{UntestedExports: []string{"Open"}}
		dir := writeGoldens(t, goldens)
		ext := metricstest.NewFake("fake", fakeRoot, pkgs, metricstest.WithDetails(details))
		metricstest.TestExtractor(t, ext, syntheticFixture(dir))
		return
	case caseModuleShape:
		// The row reports a v0 field, a v1 field that is not module-wide,
		// and no dup_blocks_cross_pkg.
		half := 0.5
		opts = append(opts, metricstest.WithModuleRow(metrics.RawMetrics{SLOC: 5, LargestFileSLOC: 5, Instability: &half}))
		goldens = syntheticGoldensWithModule()
	case caseModuleSum:
		// Two distinct blocks cannot be shared by packages whose counts
		// sum to 2.
		two := 2
		opts = append(opts, metricstest.WithModuleRow(metrics.RawMetrics{DupBlocksCrossPkg: &two}))
		goldens = syntheticGoldensWithModule()
	case caseModuleValid:
		// A negative row fails Validate, and "module" as a package collides
		// with the row: gamma is renamed to it.
		neg := -1
		opts = append(opts, metricstest.WithModuleRow(metrics.RawMetrics{DupBlocksCrossPkg: &neg}))
		pkgs[metrics.ModuleRowID] = pkgs["gamma"]
		delete(pkgs, "gamma")
		goldens[metrics.ModuleRowID] = goldens["gamma"]
		delete(goldens, "gamma")
		fx.Packages = []string{"alpha", "beta", metrics.ModuleRowID}
	default:
		t.Fatalf("unknown %s %q", caseEnv, os.Getenv(caseEnv))
	}
	fx.GoldenDir = writeGoldens(t, goldens)
	metricstest.TestExtractor(t, metricstest.NewFake("fake", fakeRoot, pkgs, opts...), fx)
}

// runSubprocess runs TestSuiteSubprocess for name, requires it to fail, and
// returns its verbose output.
func runSubprocess(t *testing.T, name string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSuiteSubprocess$", "-test.v")
	cmd.Env = append(os.Environ(), caseEnv+"="+name)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("suite on %s: want a failing exit status, got err=%v\n%s", name, err, out)
	}
	return string(out)
}

func requireContains(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestSuiteDetectsGoldenMutation(t *testing.T) {
	out := runSubprocess(t, caseGolden)
	requireContains(t, out,
		"--- FAIL: TestSuiteSubprocess/Goldens",
		"beta: sloc: golden 41, got 40 (check ",
		"COUNTING.md before regenerating)",
	)
	if n := strings.Count(out, "before regenerating"); n != 1 {
		t.Errorf("want exactly one golden difference, got %d:\n%s", n, out)
	}
}

func TestSuiteDetectsDetailsMismatch(t *testing.T) {
	out := runSubprocess(t, caseDetails)
	requireContains(t, out,
		"--- FAIL: TestSuiteSubprocess/Details",
		`beta: Details names 1 untested exports ["Open"], want untested_exports 2`,
	)
}

func TestSuiteDetectsInvariantViolation(t *testing.T) {
	out := runSubprocess(t, caseInvariant)
	requireContains(t, out,
		"--- FAIL: TestSuiteSubprocess/Invariants",
		"sum(fan_in) 4 != sum(internal_imports) 3",
	)
}

func TestSuiteDetectsRatioViolation(t *testing.T) {
	out := runSubprocess(t, caseRatio)
	requireContains(t, out,
		"--- FAIL: TestSuiteSubprocess/Invariants",
		"alpha: instability 0.9999999999999999 is not rounded to 3 decimals",
		"beta: main_sequence_distance 0.25, want |abstractness 0.5 + instability 0.5 - 1|",
	)
}

func TestSuiteDetectsModuleShape(t *testing.T) {
	out := runSubprocess(t, caseModuleShape)
	requireContains(t, out,
		"--- FAIL: TestSuiteSubprocess/ModuleRow/Shape",
		"module: sloc is 5, want 0 on the module row",
		"module: instability is 0.5, want null: it is not module-wide",
		"module: dup_blocks_cross_pkg is null, want the module-wide value",
	)
}

func TestSuiteDetectsModuleSumViolation(t *testing.T) {
	out := runSubprocess(t, caseModuleSum)
	requireContains(t, out,
		"--- FAIL: TestSuiteSubprocess/ModuleRow/Invariants",
		"sum(dup_blocks_cross_pkg) 2 < 2 * module row 2",
		"--- FAIL: TestSuiteSubprocess/Goldens",
		"module: dup_blocks_cross_pkg: golden 1, got 2 (check ",
	)
}

func TestSuiteDetectsInvalidModuleRow(t *testing.T) {
	out := runSubprocess(t, caseModuleValid)
	requireContains(t, out,
		"--- FAIL: TestSuiteSubprocess/ModuleRow/Validate",
		"module: invalid metrics: dup_blocks_cross_pkg is negative (-1)",
		`package module collides with the module row "module"`,
		"sum(dup_blocks_cross_pkg) 2 > module row -1 * 3 packages",
	)
}

func TestUpdateRewritesGoldens(t *testing.T) {
	dir := t.TempDir()
	ext := metricstest.NewFake("fake", fakeRoot, syntheticPackages())
	fx := syntheticFixture(dir)
	fx.Update = true
	metricstest.TestExtractor(t, ext, fx)

	fx.Update = false
	metricstest.TestExtractor(t, ext, fx)

	// alpha's coverage is computed, so the rewritten golden records it;
	// gamma's generated_files is not, so it is omitted rather than null.
	alpha, err := os.ReadFile(filepath.Join(dir, "alpha.json"))
	if err != nil {
		t.Fatal(err)
	}
	requireContains(t, string(alpha), `"coverage_pct": 71.5`)
	gamma, err := os.ReadFile(filepath.Join(dir, "gamma.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gamma), "null") {
		t.Errorf("rewritten golden contains null:\n%s", gamma)
	}
	if got, want := string(gamma), strings.Replace(syntheticGoldens()["gamma"], ",\n  \"generated_files\": null", "", 1); got != want {
		t.Errorf("rewritten gamma golden differs from the hand-written layout:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestGoldenNamesTrimModulePath runs the suite in update mode, then in
// compare mode, over package identifiers that are full import paths, and
// checks where the goldens land: ModulePath + "/" is trimmed, "/" becomes a
// subdirectory, and the root package is root.json. With an empty ModulePath
// the identifiers are used as they are.
func TestGoldenNamesTrimModulePath(t *testing.T) {
	const mod = "example.com/m"
	src := syntheticPackages()
	for _, tc := range []struct {
		name       string
		modulePath string
		ids        map[string]string // synthetic name -> package identifier
		files      []string          // golden files the writer must create
	}{
		{
			name:       "module path",
			modulePath: mod,
			ids:        map[string]string{"alpha": mod, "beta": mod + "/beta", "gamma": mod + "/nested/gamma"},
			files:      []string{"root.json", "beta.json", filepath.Join("nested", "gamma.json")},
		},
		{
			name:  "empty module path",
			ids:   map[string]string{"alpha": mod, "beta": mod + "/beta", "gamma": "gamma"},
			files: []string{"example.com/m.json", "example.com/m/beta.json", "gamma.json"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkgs := make(map[string]metrics.RawMetrics, len(tc.ids))
			ids := make([]string, 0, len(tc.ids))
			for short, id := range tc.ids {
				pkgs[id] = src[short]
				ids = append(ids, id)
			}
			slices.Sort(ids)
			dir := t.TempDir()
			fx := metricstest.Fixture{Root: fakeRoot, ModulePath: tc.modulePath, Packages: ids, GoldenDir: dir, Update: true}
			ext := metricstest.NewFake("fake", fakeRoot, pkgs)
			metricstest.TestExtractor(t, ext, fx)
			for _, f := range tc.files {
				if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
					t.Errorf("golden %s not written: %v", f, err)
				}
			}
			fx.Update = false
			metricstest.TestExtractor(t, ext, fx)
		})
	}
}

func TestLoadGoldenRejectsIncompleteOrUnknownFields(t *testing.T) {
	tests := map[string]string{
		"missing v0 field": `{"files": 1}`,
		"unknown field":    strings.Replace(syntheticGoldens()["beta"], `"files"`, `"filez"`, 1),
		"null v0 field":    strings.Replace(syntheticGoldens()["beta"], `"sloc": 40`, `"sloc": null`, 1),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			dir := writeGoldens(t, map[string]string{"pkg": body})
			if _, err := metricstest.LoadGolden(dir, "pkg"); err == nil {
				t.Errorf("LoadGolden accepted %s", body)
			}
		})
	}
}

func TestFakeRejectsOtherRoots(t *testing.T) {
	ext := metricstest.NewFake("fake", fakeRoot, syntheticPackages())
	if ext.Language() != "fake" {
		t.Errorf("Language() = %q, want fake", ext.Language())
	}
	if !ext.Detect(fakeRoot + "/") {
		t.Errorf("Detect does not clean the root")
	}
	if _, err := ext.Packages("/elsewhere"); err == nil {
		t.Errorf("Packages(/elsewhere) returned no error")
	}
}

// TestImportBoundary keeps metricstest importable by every extractor: it may
// depend only on the standard library and internal/metrics.
func TestImportBoundary(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("finding the go tool: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), goTool, "list", "-deps",
		"-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", metricstestPkgID)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", metricstestPkgID, err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if dep != thisPackage && dep != metricstestPkgID {
			t.Errorf("%s imports %s; only the standard library and %s are allowed", metricstestPkgID, dep, thisPackage)
		}
	}
}
