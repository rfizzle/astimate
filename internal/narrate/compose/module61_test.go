package compose

import (
	"cmp"
	"encoding/json"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/score"
)

// moduleData is the 61-package module the report mockup was rendered from:
// every package's raw metrics and the fields the mockup derived from them.
const moduleData = "../testdata/module-61.json"

// modulePackage is one package of moduleData, the fields the table
// test reads.
type modulePackage struct {
	Path        string         `json:"path"`
	Role        string         `json:"role"`
	Tier        string         `json:"tier"`
	Passes      float64        `json:"passes"`
	Days        float64        `json:"days"`
	SLOC        int            `json:"sloc"`
	Tokens      int            `json:"tokens"`
	LargestFile int            `json:"largest_file"`
	DupPct      float64        `json:"dup_pct"`
	DupBlocks   int            `json:"dup_blocks"`
	Exports     int            `json:"exports"`
	Untested    int            `json:"untested"`
	Imports     int            `json:"imports"`
	Churn       int            `json:"churn"`
	HasTests    bool           `json:"has_tests"`
	Drivers     map[string]int `json:"drivers"`
	Rebuild     int            `json:"rebuild_tokens"`
	Over        []string       `json:"over"`
	Band        string         `json:"band"`
	FuncCount   int            `json:"func_count"`
}

// moduleMetric maps the mockup's ceiling keys to metric names.
func moduleMetric(key string) string {
	return map[string]string{"sloc": "sloc", "tokens": "tokens_est", "largest_file": "largest_file_sloc",
		"exports": "exported_symbols", "imports": "internal_imports"}[key]
}

// defaultLimits are the shipped Go ceilings.
func defaultLimits() []narrate.Limit {
	notMain := []string{"library", "test-support", "generated-only"}
	return []narrate.Limit{{Metric: "sloc", Max: 1000}, {Metric: "tokens_est", Max: 16000}, {Metric: "largest_file_sloc", Max: 600},
		{Metric: "exported_symbols", Max: 60, Roles: notMain}, {Metric: "internal_imports", Max: 10, Roles: notMain}}
}

// moduleReports reads moduleData as the reports Build takes.
func moduleReports(t *testing.T) ([]modulePackage, []report.Report) {
	t.Helper()
	raw, err := os.ReadFile(moduleData)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Packages []modulePackage `json:"packages"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	reports := make([]report.Report, 0, len(data.Packages))
	for i := range data.Packages {
		p := &data.Packages[i]
		churn := p.Churn
		// The mockup has one figure, the low end; the high end is 30% above it.
		r := report.Report{PackagePath: p.Path, Role: p.Role,
			Rebuild: report.Rebuild{AgentPasses: p.Passes, HumanDays: p.Days, HumanDaysLow: p.Days,
				HumanDaysHigh: math.Round(p.Days*13) / 10, Tier: score.Tier(p.Tier), RebuildTokens: p.Rebuild},
			Metrics: metrics.RawMetrics{SLOC: p.SLOC, TokensEst: p.Tokens, LargestFileSLOC: p.LargestFile,
				ExportedSymbols: p.Exports, InternalImports: p.Imports, DuplicationPct: p.DupPct, DupBlocks: p.DupBlocks,
				UntestedExports: p.Untested, HasTests: p.HasTests, FuncCount: p.FuncCount, ChurnCommits: &churn, Role: p.Role}}
		for _, term := range []string{"spec", "volume", "unspecified"} {
			if v, ok := p.Drivers[term]; ok {
				r.Rebuild.Drivers = append(r.Rebuild.Drivers, report.Driver{Term: term, Tokens: v})
			}
		}
		reports = append(reports, r)
	}
	return data.Packages, reports
}

// TestSixtyone reproduces the mockup's derived fields from the module's
// raw metrics: every package's band, ceilings over and hotspot score
// (churn times agent passes), the hotspot order, and the hero sentences.
func TestSixtyone(t *testing.T) {
	t.Parallel()

	want, reports := moduleReports(t)
	basis := report.HumanBasis{Model: "COCOMO II, rebuild profile",
		Basic81:      report.ModelBasis{Name: "COCOMO 81", Coefficient: 2.4, Exponent: 1.05},
		COCOMOII:     report.ModelBasis{Name: "COCOMO II, rebuild profile", Coefficient: 2.3814, Exponent: 1.0528},
		DaysPerMonth: 19, UNFM: "completely_unfamiliar", Unfamiliarity: 1}
	m := Build(&Input{ModulePath: "example.com/sixtyone", Language: "go", Limits: defaultLimits(),
		ChurnWindow: 200, Packages: reports, Basis: basis})
	byPath := make(map[string]*narrate.Package, len(m.Packages))
	for i := range m.Packages {
		byPath[m.Packages[i].Path] = &m.Packages[i]
	}
	hotOf := func(w *modulePackage) float64 { return math.Round(float64(w.Churn)*w.Passes*10) / 10 }
	matched := 0
	for i := range want {
		w := &want[i]
		got := byPath[w.Path]
		over := make([]string, 0, len(w.Over))
		for _, k := range w.Over {
			over = append(over, moduleMetric(k))
		}
		if got.Band != w.Band || !slices.Equal(got.Over, over) || got.Hotspot != hotOf(w) {
			t.Errorf("%s: band %s over %q hotspot %v, want %s %q %v", w.Path, got.Band, got.Over, got.Hotspot, w.Band, over, hotOf(w))
			continue
		}
		matched++
	}
	if matched != 61 {
		t.Errorf("matched %d of %d packages, want 61", matched, len(want))
	}

	// The order is the hotspot, then the human days, then the path.
	hot := slices.Clone(want)
	slices.SortStableFunc(hot, func(a, b modulePackage) int {
		return cmp.Or(cmp.Compare(hotOf(&b), hotOf(&a)), cmp.Compare(b.Days, a.Days), strings.Compare(a.Path, b.Path))
	})
	// Start here lists the first split or watch packages of that order.
	wantHot := make([]string, 0, 5)
	for _, p := range hot {
		if len(wantHot) < 5 && byPath[p.Path].Band != narrate.BandFine {
			wantHot = append(wantHot, p.Path)
		}
	}
	if !slices.Equal(m.Hotspots, wantHot) {
		t.Errorf("hotspots = %q, want %q", m.Hotspots, wantHot)
	}
	for i, p := range hotOrder(m.Packages) {
		if p.Path != hot[i].Path {
			t.Errorf("hot order %d = %s, want %s", i, p.Path, hot[i].Path)
		}
	}

	if got, want := m.Counts, (narrate.Counts{Packages: 61, Split: 19, Watch: 11, Fine: 31, MultiSitting: m.Counts.MultiSitting, WithoutTests: m.Counts.WithoutTests,
		HumanDays: m.Counts.HumanDays, HumanDaysLow: m.Counts.HumanDays, HumanDaysHigh: m.Counts.HumanDaysHigh,
		HumanTime: m.Counts.HumanTime, HumanBasis: narrate.HumanBasis(basis),
		HumanBasisShort: narrate.HumanBasisShort(basis)}); got != want {
		t.Errorf("counts = %+v, want %+v", got, want)
	}
	if want := "Sixtyone has 19 packages that need a split. Start with m/p07."; m.Headline != want {
		t.Errorf("headline = %q, want %q", m.Headline, want)
	}
	if want := "61 packages; " + strconv.Itoa(m.Counts.MultiSitting) +
		" need more than one agent sitting to rebuild from their tests. For one engineer the whole module is about " +
		m.Counts.HumanTime + " of rebuild time. " +
		"31 are fine, 11 need watching, and 19 need a split. The hottest of those changed 41 times in the last " +
		"200 commits and 42.6% of it is copied code."; m.Lead != want {
		t.Errorf("lead = %q, want %q", m.Lead, want)
	}
	c := byPath["m/p07"]
	if want := "It is over all five of its limits (size, tokens, largest file, exports, imports), and 42.6% of its " +
		"lines are copies of each other, and 16 of its 203 exports have no test."; c.Story != want {
		t.Errorf("story = %q, want %q", c.Story, want)
	}
	if want := "Extract the copied code first, then split the package along its responsibilities."; c.Todo != want {
		t.Errorf("todo = %q, want %q", c.Todo, want)
	}
	if want := "Too big to rebuild as it stands: an agent would need about 8 sittings, so the package has to be split " +
		"first. For one engineer, about 1 to 2 engineer-years."; c.Rebuild.Say != want {
		t.Errorf("rebuild say = %q, want %q", c.Rebuild.Say, want)
	}
	if want := []narrate.Share{{Term: "spec", Label: "the tests it must satisfy", Tokens: 198254, Pct: 68},
		{Term: "volume", Label: "the amount of code", Tokens: 88442, Pct: 30}}; !slices.Equal(c.Rebuild.Drivers, want) {
		t.Errorf("drivers = %+v, want %+v", c.Rebuild.Drivers, want)
	}
}
