package main

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/metrics"
)

// defaultConfig parses the embedded default configuration.
func defaultConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestCollectModule runs the per-module collection on two modules that need
// no network, the Go fixture and this repository, writes the rows as
// packages.jsonl, and checks every line carries the pooled fields, and
// that the module row was measured (checkModuleRow).
func TestCollectModule(t *testing.T) {
	if testing.Short() {
		t.Skip("loads whole modules")
	}
	cfg := defaultConfig(t)
	tests := []struct {
		name, dir, module, wantPkg string
		// wantCross is the module row's dup_blocks_cross_pkg, or -1 for
		// any value.
		wantCross int
	}{
		// The fixture's a.Checksum and b.Digest share one block.
		{"fixture", "../../testdata/go/fixture", "example.com/fixture", "example.com/fixture/hub", 1},
		{"this repository", "../..", "github.com/rfizzle/astimate", "github.com/rfizzle/astimate/internal/engine", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const commit = "0123456789abcdef0123456789abcdef01234567"
			res, err := collectModule(t.Context(), tt.dir, commit, cfg, slog.New(slog.DiscardHandler), false)
			if err != nil {
				t.Fatal(err)
			}
			rows, modPath := res.Rows, res.ModPath
			if modPath != tt.module {
				t.Errorf("module path %q, want %q", modPath, tt.module)
			}
			checkModuleRow(t, res.ModuleRow, tt.module, commit, len(rows), tt.wantCross)
			path := filepath.Join(t.TempDir(), "packages.jsonl")
			if err := writeRows(path, rows); err != nil {
				t.Fatal(err)
			}
			lines := readLines(t, path)
			if len(lines) != len(rows) || len(lines) == 0 {
				t.Fatalf("wrote %d lines for %d rows", len(lines), len(rows))
			}
			var pkgs []string
			for _, line := range lines {
				var got map[string]json.RawMessage
				if err := json.Unmarshal([]byte(line), &got); err != nil {
					t.Fatal(err)
				}
				for _, k := range []string{"module", "commit", "package", "metrics", "agent_passes", "human_days"} {
					if _, ok := got[k]; !ok {
						t.Errorf("row %s lacks %q", line, k)
					}
				}
				var r Row
				if err := json.Unmarshal([]byte(line), &r); err != nil {
					t.Fatal(err)
				}
				if r.Module != tt.module || r.Commit != commit || !strings.HasPrefix(r.Package, tt.module) {
					t.Errorf("row identifiers %q %q %q", r.Module, r.Commit, r.Package)
				}
				// The coupling metrics ride along in the metrics object so
				// the calibration report can show their distribution.
				var metricKeys map[string]json.RawMessage
				if err := json.Unmarshal(got["metrics"], &metricKeys); err != nil {
					t.Fatal(err)
				}
				for _, k := range []string{"instability", "abstractness", "main_sequence_distance"} {
					if _, ok := metricKeys[k]; !ok {
						t.Errorf("row %s metrics lack %q", r.Package, k)
					}
				}
				if r.Package == "example.com/fixture/hub" && (r.Metrics.Instability == nil || *r.Metrics.Instability != 0) {
					t.Errorf("hub instability = %v, want 0", r.Metrics.Instability)
				}
				checkFuncCognitive(t, &r)
				pkgs = append(pkgs, r.Package)
			}
			if !slices.Contains(pkgs, tt.wantPkg) {
				t.Errorf("packages %v lack %s", pkgs, tt.wantPkg)
			}
			if !slices.IsSorted(pkgs) {
				t.Errorf("packages not sorted: %v", pkgs)
			}
		})
	}
}

// TestCollectModuleRowOnly collects the fixture's module row alone, writes
// it as modules.jsonl, and checks the line carries the fields the fitter
// reads, as a packages.jsonl row does, and the pass's cost.
func TestCollectModuleRowOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("loads a module")
	}
	const commit = "0123456789abcdef0123456789abcdef01234567"
	res, err := collectModule(t.Context(), "../../testdata/go/fixture", commit, defaultConfig(t),
		slog.New(slog.DiscardHandler), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 0 {
		t.Errorf("got %d package rows, want none", len(res.Rows))
	}
	checkModuleRow(t, res.ModuleRow, "example.com/fixture", commit, -1, 1)
	path := filepath.Join(t.TempDir(), "modules.jsonl")
	if err := writeRows(path, []ModuleRow{*res.ModuleRow}); err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("wrote %d lines, want 1", len(lines))
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"module", "commit", "package", "metrics", "packages", "cost"} {
		if _, ok := got[k]; !ok {
			t.Errorf("module row %s lacks %q", lines[0], k)
		}
	}
	var cost map[string]json.RawMessage
	if err := json.Unmarshal(got["cost"], &cost); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"load_ms", "pass_ms", "pass_alloc_bytes", "pass_peak_heap_bytes"} {
		if _, ok := cost[k]; !ok {
			t.Errorf("module row cost %s lacks %q", got["cost"], k)
		}
	}
}

// checkModuleRow checks a collected module row: identified as the module
// row of module at commit, with dup_blocks_cross_pkg set (to want unless it
// is -1), its package count pkgs unless that is -1, and the pass's cost
// recorded.
func checkModuleRow(t *testing.T, r *ModuleRow, module, commit string, pkgs, want int) {
	t.Helper()
	if r == nil {
		t.Fatal("no module row")
	}
	if r.Module != module || r.Commit != commit || r.Package != metrics.ModuleRowID {
		t.Errorf("module row identifiers %q %q %q", r.Module, r.Commit, r.Package)
	}
	switch got := r.Metrics.DupBlocksCrossPkg; {
	case got == nil:
		t.Error("module row has no dup_blocks_cross_pkg")
	case want >= 0 && *got != want:
		t.Errorf("module row dup_blocks_cross_pkg = %d, want %d", *got, want)
	}
	if r.Packages <= 0 || (pkgs >= 0 && r.Packages != pkgs) {
		t.Errorf("module row packages = %d, want %d", r.Packages, pkgs)
	}
	if r.Cost.PassAllocBytes == 0 || r.Cost.LoadMS < 0 || r.Cost.PassMS < 0 {
		t.Errorf("module row cost %+v not recorded", r.Cost)
	}
}

// TestCollectStdlib measures the standard library in one load and keeps
// the rows of two packages, one of them imported across the library, and
// reports one that does not exist, which is not fatal.
func TestCollectStdlib(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the whole standard library")
	}
	cfg := defaultConfig(t)
	pkgs := []string{"unicode/utf16", "errors", "example.invalid/nope"}
	rows, failed, err := collectStdlib(t.Context(), pkgs, "go1.test", cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Skipf("toolchain has no usable GOROOT sources: %v", err)
	}
	if len(failed) != 1 || failed[0].Package != "example.invalid/nope" {
		t.Errorf("failed = %+v, want only example.invalid/nope", failed)
	}
	if len(rows) != 2 || rows[0].Package != "errors" || rows[1].Package != "unicode/utf16" {
		t.Fatalf("rows = %+v, want errors and unicode/utf16 in order", rows)
	}
	for _, r := range rows {
		if r.Module != stdlibModule || r.Commit != "go1.test" || r.Metrics.SLOC == 0 || r.AgentPasses <= 0 {
			t.Errorf("row %+v", r)
		}
	}
	if rows[0].Metrics.FanIn == 0 {
		t.Error("errors has fan_in 0, want its standard-library importers counted")
	}
	for i := range rows {
		checkFuncCognitive(t, &rows[i])
	}
}

// checkFuncCognitive checks a row's per-function cognitive counts agree
// with its package metrics: one count per function, summing to
// cognitive_total, and absent exactly when the package has no functions.
func checkFuncCognitive(t *testing.T, r *Row) {
	t.Helper()
	n, total := 0, 0
	for v, c := range r.FuncCognitive {
		n += c
		total += v * c
	}
	if n != r.Metrics.FuncCount || total != r.Metrics.CognitiveTotal {
		t.Errorf("%s: func_cognitive counts %d functions totalling %d, metrics say %d and %d",
			r.Package, n, total, r.Metrics.FuncCount, r.Metrics.CognitiveTotal)
	}
	if (r.FuncCognitive == nil) != (r.Metrics.FuncCount == 0) {
		t.Errorf("%s: func_cognitive %v with func_count %d", r.Package, r.FuncCognitive, r.Metrics.FuncCount)
	}
}

func TestCognitiveCounts(t *testing.T) {
	tests := []struct {
		name string
		fns  []metrics.FunctionInfo
		want map[int]int
	}{
		{"none", nil, nil},
		{"counts by value", []metrics.FunctionInfo{{Cognitive: 0}, {Cognitive: 3}, {Cognitive: 0}, {Cognitive: 12}},
			map[int]int{0: 2, 3: 1, 12: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cognitiveCounts(tt.fns); !maps.Equal(got, tt.want) || (got == nil) != (tt.want == nil) {
				t.Errorf("cognitiveCounts = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStdPackagesExcludeVendor(t *testing.T) {
	pkgs, err := stdPackages(t.Context())
	if err != nil {
		t.Skipf("go list std: %v", err)
	}
	if !slices.Contains(pkgs, "net/http") {
		t.Error("net/http missing")
	}
	for _, p := range pkgs {
		if strings.HasPrefix(p, "vendor/") {
			t.Errorf("vendored package %s listed", p)
		}
	}
}

// readLines returns the lines of the file at path.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}
