package html_test

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/narrate/compose"
	"github.com/rfizzle/astimate/internal/narrate/html"
	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/report/details"
	"github.com/rfizzle/astimate/internal/score"
)

// overwatchData is the 61-package module the report mockup was drawn
// from, with every number the mockup shows.
const overwatchData = "../../../docs/design/report-mockup-data-2026-10-14.json"

// owFinding is a violation or warning of overwatchData.
type owFinding struct {
	Metric     string           `json:"metric"`
	Base       *float64         `json:"base"`
	Head       float64          `json:"head"`
	Limit      string           `json:"limit"`
	Suggestion string           `json:"suggestion"`
	Location   *report.Location `json:"location"`
}

// owFunc is one of a package's hardest functions in overwatchData.
type owFunc struct {
	Name string `json:"name"`
	File string `json:"file"`
	Line int    `json:"line"`
	Cog  int    `json:"cog"`
	Cyc  int    `json:"cyc"`
}

// owPackage is one package of overwatchData.
type owPackage struct {
	Path, Role, Tier                           string
	Passes                                     float64
	Days                                       float64
	SLOC                                       int `json:"sloc"`
	Tokens                                     int
	LargestFile                                int     `json:"largest_file"`
	DupPct                                     float64 `json:"dup_pct"`
	DupBlocks                                  int     `json:"dup_blocks"`
	DupCode                                    int     `json:"dup_code"`
	CogP90                                     int     `json:"cog_p90"`
	CycP90                                     int     `json:"cyc_p90"`
	Nesting, Exports, Untested, Imports, Churn int
	Authors                                    int
	FanIn                                      int  `json:"fan_in"`
	HasTests                                   bool `json:"has_tests"`
	TestFuncs                                  int  `json:"test_funcs"`
	BaseSLOC                                   *int `json:"base_sloc"`
	Violations, Warnings                       []owFinding
	Band                                       string
	Hot                                        float64
	Drivers                                    map[string]int
	RebuildTokens                              int      `json:"rebuild_tokens"`
	FuncCount                                  int      `json:"func_count"`
	FnEasy                                     int      `json:"fn_easy"`
	FnTricky                                   int      `json:"fn_tricky"`
	FnHard                                     int      `json:"fn_hard"`
	TopFns                                     []owFunc `json:"top_fns"`
	Chunks                                     []narrate.Chunk
}

// overwatch is overwatchData decoded.
type overwatch struct {
	Module    string `json:"module"`
	Head      string `json:"head"`
	Packages  []owPackage
	ModuleRow struct {
		Cross     int `json:"cross_blocks"`
		BaseCross int `json:"base_cross"`
	} `json:"module_row"`
}

// findings converts overwatch findings to the gate's.
func findings(fs []owFinding) []report.Finding {
	out := make([]report.Finding, 0, len(fs))
	for _, f := range fs {
		out = append(out, report.Finding{Metric: f.Metric, Base: f.Base, Head: f.Head, Limit: f.Limit,
			Suggestion: f.Suggestion, Location: f.Location})
	}
	return out
}

// functions are a package's functions as the extractor would list them:
// its hardest first, then enough others in each bucket, at the bucket's
// lowest score, to fill the mockup's counts.
func functions(p *owPackage) []details.Function {
	fns := make([]details.Function, 0, p.FuncCount)
	left := map[int]int{0: p.FnEasy, 11: p.FnTricky, 21: p.FnHard}
	for _, f := range p.TopFns {
		fns = append(fns, details.Function{Name: f.Name, File: f.File, Line: f.Line, Cognitive: f.Cog, Cyclomatic: f.Cyc})
		switch {
		case f.Cog > 20:
			left[21]--
		case f.Cog > 10:
			left[11]--
		default:
			left[0]--
		}
	}
	for _, cog := range []int{0, 11, 21} {
		for i := range left[cog] {
			fns = append(fns, details.Function{Name: "f" + strconv.Itoa(cog) + "_" + strconv.Itoa(i), Cognitive: cog, Cyclomatic: 1})
		}
	}
	return fns
}

// overwatchModule builds the module report of overwatchData, with the
// change the mockup's "This change" tab shows.
func overwatchModule(t *testing.T) (*overwatch, *narrate.Module) {
	t.Helper()
	raw, err := os.ReadFile(overwatchData)
	if err != nil {
		t.Fatal(err)
	}
	var ow overwatch
	if err := json.Unmarshal(raw, &ow); err != nil {
		t.Fatal(err)
	}
	in := &compose.Input{ModulePath: ow.Module, Language: "go", Head: ow.Head, Base: "bce3876a", ChurnWindow: 200, MinTokens: 60,
		Limits: []narrate.Limit{{Metric: "sloc", Max: 1000}, {Metric: "tokens_est", Max: 16000}, {Metric: "largest_file_sloc", Max: 600},
			{Metric: "exported_symbols", Max: 60, Roles: []string{"library", "test-support", "generated-only"}},
			{Metric: "internal_imports", Max: 10, Roles: []string{"library", "test-support", "generated-only"}}},
		AstimateVersion: "test", ConfigVersion: "thresholds-2026-10-14b"}
	cross, baseCross := ow.ModuleRow.Cross, ow.ModuleRow.BaseCross
	baseline := &report.Baseline{Ref: "bce3876a"}
	in.Check = &report.Check{Module: &report.CheckedPackage{Report: report.Report{PackagePath: "<module>",
		Metrics:  metrics.RawMetrics{DupBlocksCrossPkg: &cross},
		Baseline: &report.Baseline{Ref: "bce3876a", Metrics: metrics.RawMetrics{DupBlocksCrossPkg: &baseCross}}}}}
	for i := range ow.Packages {
		p := &ow.Packages[i]
		churn, authors, code := p.Churn, p.Authors, p.DupCode
		r := report.Report{PackagePath: p.Path, Role: p.Role,
			Rebuild: report.Rebuild{AgentPasses: p.Passes, HumanDays: p.Days, Tier: score.Tier(p.Tier), RebuildTokens: p.RebuildTokens},
			Metrics: metrics.RawMetrics{SLOC: p.SLOC, TokensEst: p.Tokens, LargestFileSLOC: p.LargestFile, ExportedSymbols: p.Exports,
				InternalImports: p.Imports, DuplicationPct: p.DupPct, DupBlocks: p.DupBlocks, DupBlocksCode: &code,
				UntestedExports: p.Untested, HasTests: p.HasTests, TestFuncs: p.TestFuncs, FuncCount: p.FuncCount,
				CognitiveP90: p.CogP90, CyclomaticP90: p.CycP90, MaxNesting: p.Nesting, FanIn: p.FanIn,
				ChurnCommits: &churn, ChurnAuthors: &authors, Role: p.Role},
			Details: &details.Details{Functions: functions(p)}}
		for _, c := range p.Chunks {
			r.Details.Duplicates = append(r.Details.Duplicates, details.Span{File: c.File, StartLine: c.Start, EndLine: c.End})
		}
		for _, term := range []string{"spec", "volume", "unspecified"} {
			if v, ok := p.Drivers[term]; ok {
				r.Rebuild.Drivers = append(r.Rebuild.Drivers, report.Driver{Term: term, Tokens: v})
			}
		}
		in.Packages = append(in.Packages, r)
		// The mockup's changed packages: a baseline, and a finding or a
		// change in size.
		if p.BaseSLOC != nil && (len(p.Violations) > 0 || len(p.Warnings) > 0 || p.SLOC != *p.BaseSLOC) {
			cr := r
			cr.Violations, cr.Warnings, cr.Baseline = findings(p.Violations), findings(p.Warnings), baseline
			in.Check.Packages = append(in.Check.Packages, report.CheckedPackage{Report: cr})
		}
	}
	m := compose.Build(in)
	return &ow, &m
}

// attrRE finds a mark's numbers on the page by its data-i.
func circleAt(t *testing.T, page, pattern string) []float64 {
	t.Helper()
	got := regexp.MustCompile(pattern).FindStringSubmatch(page)
	if got == nil {
		t.Fatalf("page has no match for %s", pattern)
	}
	out := make([]float64, 0, len(got)-1)
	for _, s := range got[1:] {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

// TestOverwatch renders the mockup's module and checks the page shows the
// mockup's numbers: the bands, the three hotspots, the bubbles where d3
// packs them, the scatter where the mockup's scales put it, the longest
// repeats and hardest functions, and the change; and that it stays under
// 1 MB with the fonts embedded. With HTML_REPORT_OUT set it also writes
// the page there.
func TestOverwatch(t *testing.T) {
	t.Parallel()

	ow, m := overwatchModule(t)
	var buf bytes.Buffer
	if err := html.Write(&buf, m); err != nil {
		t.Fatal(err)
	}
	page := buf.String()
	if out := os.Getenv("HTML_REPORT_OUT"); out != "" {
		if err := os.WriteFile(filepath.Clean(out), buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("overwatch page: %d bytes for %d packages", buf.Len(), len(m.Packages))
	if buf.Len() >= 1<<20 {
		t.Errorf("page is %d bytes, want under 1 MB", buf.Len())
	}
	for _, bad := range []string{"http://", "https://", "<link", " src="} {
		if strings.Contains(page, bad) {
			t.Errorf("page contains %q; it must load nothing", bad)
		}
	}
	for _, id := range []string{"repo", "change"} {
		if !strings.Contains(page, `<section id="`+id+`"`) || !strings.Contains(page, `data-tab="`+id+`"`) {
			t.Errorf("page has no %s tab", id)
		}
	}

	// Bands, as the stickers and the bubbles colour them.
	want := map[string]int{}
	for _, p := range ow.Packages {
		want[p.Band]++
	}
	if c := m.Counts; c.Split != want["split"] || c.Watch != want["watch"] || c.Fine != want["fine"] || c.Split != 19 {
		t.Errorf("counts = %+v, want the mockup's %v", c, want)
	}
	bub := page[strings.Index(page, `id="bubbles"`):strings.Index(page, `id="scatter"`)]
	for band, n := range want {
		if got := strings.Count(bub, `class="`+band+`"/>`); got != n {
			t.Errorf("%d %s bubbles, want %d", got, band, n)
		}
	}

	// The three hotspots, in the mockup's order.
	hot := slices.Clone(ow.Packages)
	slices.SortStableFunc(hot, func(a, b owPackage) int { return cmp.Compare(b.Hot, a.Hot) })
	steps := regexp.MustCompile(`<div class="name">([^<]+)</div>`).FindAllStringSubmatch(page, -1)
	if len(steps) != 3 {
		t.Fatalf("got %d steps, want 3", len(steps))
	}
	for i, s := range steps {
		if s[1] != hot[i].Path {
			t.Errorf("step %d = %s, want %s", i+1, s[1], hot[i].Path)
		}
	}

	// Bubbles where d3.pack puts them; the scatter on the mockup's scales.
	raw, err := os.ReadFile("internal/layout/testdata/d3-pack-overwatch.json")
	if err != nil {
		t.Fatal(err)
	}
	var d3 [][3]float64
	if err := json.Unmarshal(raw, &d3); err != nil {
		t.Fatal(err)
	}
	maxChurn, maxDays, maxTokens := 0.0, 0.0, 0.0
	for _, p := range ow.Packages {
		maxChurn, maxDays, maxTokens = math.Max(maxChurn, float64(p.Churn)), math.Max(maxDays, p.Days), math.Max(maxTokens, float64(p.Tokens))
	}
	for i, p := range ow.Packages {
		g := circleAt(t, bub, fmt.Sprintf(`translate\(([\d.]+),([\d.]+)\)" data-i="%d"><title>[^<]*</title><circle r="([\d.]+)"`, i))
		for k := range 3 {
			if math.Abs(g[k]-d3[i][k]) > 0.006 {
				t.Errorf("%s bubble %v, want d3's %v", p.Path, g, d3[i])
				break
			}
		}
		sc := circleAt(t, page, fmt.Sprintf(`cx="([\d.]+)" cy="([\d.]+)" r="([\d.]+)" data-i="%d"`, i))
		x := 70 + float64(p.Churn)/(maxChurn*1.05)*900
		y := 500 - math.Sqrt(p.Days)/math.Sqrt(maxDays*1.05)*470
		r := 4 + math.Sqrt(float64(p.Tokens))/math.Sqrt(maxTokens)*30
		if math.Abs(sc[0]-x) > 0.006 || math.Abs(sc[1]-y) > 0.006 || math.Abs(sc[2]-r) > 0.006 {
			t.Errorf("%s scatter %v, want %v %v %v", p.Path, sc, x, y, r)
		}
	}

	// Longest repeats and hardest functions, in the widgets.
	byPath := map[string]*narrate.Package{}
	for i := range m.Packages {
		byPath[m.Packages[i].Path] = &m.Packages[i]
	}
	for _, p := range ow.Packages {
		got := byPath[p.Path]
		if wantChunks := p.Chunks[:min(2, len(p.Chunks))]; !slices.Equal(got.Copies.Chunks, wantChunks) {
			t.Errorf("%s chunks = %v, want %v", p.Path, got.Copies.Chunks, wantChunks)
		}
		names := make([]string, 0, 3)
		for _, f := range got.Complexity.Hardest {
			names = append(names, f.Name)
		}
		wantNames := make([]string, 0, 3)
		for _, f := range p.TopFns {
			wantNames = append(wantNames, f.Name)
		}
		if !slices.Equal(names[:min(len(names), len(wantNames))], wantNames) {
			t.Errorf("%s hardest = %q, want %q", p.Path, names, wantNames)
		}
		wantBuckets := narrate.Buckets{Easy: p.FnEasy, Care: p.FnTricky, Hard: p.FnHard}
		if b := got.Complexity.Buckets; (b == nil) != (p.FuncCount == 0) || b != nil && *b != wantBuckets {
			t.Errorf("%s buckets = %+v, want %+v", p.Path, b, wantBuckets)
		}
		for _, f := range p.TopFns {
			if !strings.Contains(page, "<span>"+f.Name+"</span>") {
				t.Errorf("page lacks %s's function %s", p.Path, f.Name)
			}
		}
	}
	for _, want := range []string{"Longest repeat: <b>sessions.go:265-345</b> (81 lines), again at <b>sessions.go:478-558</b>.",
		`<span class="sc split-t">56</span><span class="where">reconciliation.go:275</span><span class="where">47 paths</span>`} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}

	// The change: the mockup's failing packages, most findings first.
	failed := 0
	for _, p := range ow.Packages {
		if len(p.Violations) > 0 {
			failed++
		}
	}
	if c := m.Change; c == nil || c.Failed != failed || c.Verdict != "FAIL" || c.Changed != 28 {
		t.Fatalf("change = %+v, want %d failing of 28 changed", c, failed)
	}
	if got := strings.Count(page, `<span class="pill fail">FAIL</span>`); got != failed {
		t.Errorf("%d failing cards, want %d", got, failed)
	}
	if !strings.Contains(page, "<b class=\"num\">185→199</b>copies between packages") {
		t.Error("page lacks the cross-package copies sticker")
	}
}
