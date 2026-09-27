package main

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
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
// packages.jsonl, and checks every line carries the pooled fields.
func TestCollectModule(t *testing.T) {
	if testing.Short() {
		t.Skip("loads whole modules")
	}
	cfg := defaultConfig(t)
	tests := []struct {
		name, dir, module, wantPkg string
	}{
		{"fixture", "../../testdata/go/fixture", "example.com/fixture", "example.com/fixture/hub"},
		{"this repository", "../..", "github.com/rfizzle/astimate", "github.com/rfizzle/astimate/internal/engine"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const commit = "0123456789abcdef0123456789abcdef01234567"
			rows, modPath, _, err := collectModule(t.Context(), tt.dir, commit, cfg, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatal(err)
			}
			if modPath != tt.module {
				t.Errorf("module path %q, want %q", modPath, tt.module)
			}
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
