package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/score"
)

// runRankJSON runs `astimate rank --json` on the fixture with extra args and
// decodes the rows, failing the test on a non-zero exit.
func runRankJSON(t *testing.T, extra ...string) []report.Row {
	t.Helper()
	args := append([]string{"rank", fixtureDir, "--json"}, extra...)
	var stdout, stderr bytes.Buffer
	if got := run(args, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, stderr.String())
	}
	var rows []report.Row
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rows); err != nil {
		t.Fatalf("decoding rows: %v\n%s", err, stdout.String())
	}
	return rows
}

func rowPaths(rows []report.Row) []string {
	out := make([]string, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].Path)
	}
	return out
}

// sortValue is the value --sort key orders rows by.
func sortValue(r *report.Row, key string) float64 {
	switch key {
	case report.SortDays:
		return r.HumanDays
	case report.SortFanIn:
		return float64(r.FanIn)
	case report.SortTokens:
		return float64(r.TokensEst)
	case report.SortDuplication:
		return r.DuplicationPct
	default:
		return r.AgentPasses
	}
}

func TestRankSortKeys(t *testing.T) {
	t.Parallel()

	for _, key := range report.SortKeys() {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			rows := runRankJSON(t, "--sort", key)
			if got := rowPaths(rows); !slices.Equal(sorted(got...), fixturePackages()) {
				t.Fatalf("rows = %q, want one per fixture package %q", got, fixturePackages())
			}
			ordered := slices.IsSortedFunc(rows, func(a, b report.Row) int {
				if c := cmp.Compare(sortValue(&b, key), sortValue(&a, key)); c != 0 {
					return c
				}
				return strings.Compare(a.Path, b.Path)
			})
			if !ordered {
				t.Errorf("rows %q are not sorted by %s descending with path ascending", rowPaths(rows), key)
			}
		})
	}
}

func TestRankDefaultOrder(t *testing.T) {
	t.Parallel()

	got := rowPaths(runRankJSON(t))
	trivial := slices.Index(got, "trivial")
	for _, pkg := range []string{"dupes", "hub", "hidden"} {
		if i := slices.Index(got, pkg); i < 0 || i > trivial {
			t.Errorf("rank order %q: %s is not above trivial", got, pkg)
		}
	}
}

func TestRankDuplicationFirst(t *testing.T) {
	t.Parallel()

	if got := rowPaths(runRankJSON(t, "--sort", "duplication")); len(got) == 0 || got[0] != "dupes" {
		t.Errorf("--sort duplication order = %q, want dupes first", got)
	}
}

func TestRankTop(t *testing.T) {
	t.Parallel()

	all := runRankJSON(t)
	for _, n := range []int{1, 2, 100} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			rows := runRankJSON(t, "--top", strconv.Itoa(n))
			want := min(len(all), n)
			if len(rows) != want {
				t.Fatalf("--top %d returned %d rows, want %d", n, len(rows), want)
			}
			if !slices.Equal(rows, all[:want]) {
				t.Errorf("--top %d rows = %q, want the first %d of %q", n, rowPaths(rows), want, rowPaths(all))
			}
		})
	}
}

func TestRankTable(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	args := []string{"rank", fixtureDir, "--top", "2"}
	if got := run(args, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, stderr.String())
	}
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != 3 || strings.Join(strings.Fields(lines[0]), " ") != "PATH PASSES DAYS TIER FAN_IN TOKENS DUP%" {
		t.Errorf("table output = %q, want the header and two rows", stdout.String())
	}
}

func TestRankUsageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"unknown sort key", []string{"rank", fixtureDir, "--sort", "size"}, exitUsage},
		{"negative top", []string{"rank", fixtureDir, "--top", "-1"}, exitUsage},
		{"two roots", []string{"rank", fixtureDir, fixtureDir}, exitUsage},
		{"unknown tokenizer", []string{"rank", fixtureDir, "--tokenizer", "bpe"}, exitUsage},
		{"no module", []string{"rank", t.TempDir()}, exitAnalysis},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			if got := run(tt.args, &stdout, &stderr); got != tt.want {
				t.Errorf("run(%q) exit code = %d, want %d; stderr = %q", tt.args, got, tt.want, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("run(%q) stdout = %q, want empty", tt.args, stdout.String())
			}
		})
	}
}

// errExtract is the failure the counting extractor injects.
var errExtract = errors.New("injected extract failure")

// countingExtractor wraps an extractor, counts Packages and Extract calls
// and fails Extract for one package.
type countingExtractor struct {
	metrics.Extractor
	fail     string
	packages int
	extracts map[string]int
}

func (c *countingExtractor) Packages(root string) ([]string, error) {
	c.packages++
	return c.Extractor.Packages(root)
}

func (c *countingExtractor) Extract(ctx context.Context, mod *metrics.ModuleContext, pkg string) (metrics.RawMetrics, error) {
	c.extracts[pkg]++
	if pkg == c.fail {
		return metrics.RawMetrics{}, errExtract
	}
	return c.Extractor.Extract(ctx, mod, pkg)
}

func TestRankModulePartialFailure(t *testing.T) {
	t.Parallel()

	const root, modPath = "/mod", "example.com/m"
	pkgs := map[string]metrics.RawMetrics{
		modPath:              {TokensEst: 100},
		modPath + "/big":     {TokensEst: 40000, FanIn: 2},
		modPath + "/broken":  {TokensEst: 900},
		modPath + "/x/small": {TokensEst: 10},
	}
	tests := []struct {
		name      string
		fail      string
		wantCode  int
		wantPaths []string
	}{
		{"all succeed", "", exitOK, []string{"big", ".", "broken", "x/small"}},
		{"one fails", modPath + "/broken", exitAnalysis, []string{"big", ".", "x/small"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ext := &countingExtractor{
				Extractor: metricstest.NewFake("go", root, pkgs),
				fail:      tt.fail,
				extracts:  map[string]int{},
			}
			mod := &metrics.ModuleContext{Root: root, ModulePath: modPath}
			var stdout, stderr bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&stderr, nil))
			opts := rankOptions{sortKey: report.SortPasses, asJSON: true}
			got := rankModule(context.Background(), ext, mod, rankParams(), opts, &stdout, logger)
			if got != tt.wantCode {
				t.Errorf("rankModule exit code = %d, want %d; stderr = %q", got, tt.wantCode, stderr.String())
			}

			// One Packages call and one Extract per package: with the Go
			// extractor these share a single module load, which
			// TestOneLoadPerModule in internal/lang/golang proves.
			if ext.packages != 1 {
				t.Errorf("Packages called %d times, want 1", ext.packages)
			}
			for pkg := range pkgs {
				if ext.extracts[pkg] != 1 {
					t.Errorf("Extract(%s) called %d times, want 1", pkg, ext.extracts[pkg])
				}
			}

			var rows []report.Row
			if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
				t.Fatalf("decoding rows: %v\n%s", err, stdout.String())
			}
			if got := rowPaths(rows); !slices.Equal(got, tt.wantPaths) {
				t.Errorf("rows = %q, want %q", got, tt.wantPaths)
			}
			if tt.fail != "" && (!strings.Contains(stderr.String(), "path=broken") ||
				!strings.Contains(stderr.String(), errExtract.Error())) {
				t.Errorf("stderr = %q, want the failed path and its error", stderr.String())
			}
		})
	}
}

func TestRankModuleListFailure(t *testing.T) {
	t.Parallel()

	ext := metricstest.NewFake("go", "/mod", nil)
	mod := &metrics.ModuleContext{Root: "/elsewhere", ModulePath: "example.com/m"}
	var stdout, stderr bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&stderr, nil))
	got := rankModule(context.Background(), ext, mod, rankParams(), rankOptions{sortKey: report.SortPasses}, &stdout, logger)
	if got != exitAnalysis || stdout.Len() != 0 {
		t.Errorf("rankModule = %d with stdout %q, want %d and empty stdout", got, stdout.String(), exitAnalysis)
	}
}

// rankParams are the SPEC.md 7.2 and 7.3 default rebuild parameters.
func rankParams() score.RebuildParams {
	return score.RebuildParams{
		ContextBudget:           25000,
		TokensPerExport:         40,
		TokensPerUntestedExport: 800,
		TokensPerHiddenState:    400,
		SuperlinearExponent:     1.3,
		CocomoA:                 2.4,
		CocomoB:                 1.05,
		DaysPerMonth:            19,
		Tiers:                   score.Tiers{OnePassMax: 1, FewPassesMax: 3},
	}
}

func TestModulePathRel(t *testing.T) {
	t.Parallel()

	tests := []struct{ importPath, want string }{
		{"example.com/m", "."},
		{"example.com/m/a", "a"},
		{"example.com/m/a/b", "a/b"},
	}
	for _, tt := range tests {
		if got := modulePathRel("example.com/m", tt.importPath); got != tt.want {
			t.Errorf("modulePathRel(%q) = %q, want %q", tt.importPath, got, tt.want)
		}
	}
}
