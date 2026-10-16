package compose

import (
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/report/details"
	"github.com/rfizzle/astimate/internal/score"
)

// row is a package report with the fields the sentences read.
type row struct {
	path                     string
	tier                     string
	passes, days             float64
	sloc, exports, untested  int
	dup                      float64
	dupBlocks, testFuncs     int
	hasTests                 bool
	churn                    *int
	authors                  int
	fanIn, cogP90, nest      int
	functions                []details.Function
	duplicates               []details.Span
	specTokens, volumeTokens int
}

// report returns r as a package report.
func (r row) report() report.Report {
	m := metrics.RawMetrics{SLOC: r.sloc, ExportedSymbols: r.exports, UntestedExports: r.untested, DuplicationPct: r.dup,
		DupBlocks: r.dupBlocks, TestFuncs: r.testFuncs, HasTests: r.hasTests, ChurnCommits: r.churn, FanIn: r.fanIn,
		CognitiveP90: r.cogP90, MaxNesting: r.nest, FuncCount: len(r.functions)}
	if r.churn != nil {
		m.ChurnAuthors = &r.authors
	}
	tier := r.tier
	if tier == "" {
		tier = "ONE_PASS"
	}
	rep := report.Report{PackagePath: r.path, Metrics: m,
		Rebuild: report.Rebuild{AgentPasses: r.passes, HumanDays: r.days, Tier: score.Tier(tier),
			RebuildTokens: r.specTokens + r.volumeTokens,
			Drivers:       []report.Driver{{Term: "spec", Tokens: r.specTokens}, {Term: "volume", Tokens: r.volumeTokens}}}}
	if r.functions != nil || r.duplicates != nil {
		rep.Details = &details.Details{Functions: r.functions, Duplicates: r.duplicates}
	}
	return rep
}

// narrated builds the module report of rs and returns it with its
// packages by path.
func narrated(rs ...row) (narrate.Module, map[string]*narrate.Package) {
	reports := make([]report.Report, 0, len(rs))
	for _, r := range rs {
		reports = append(reports, r.report())
	}
	m := Build(&Input{ModulePath: "example.com/shop", Language: "go", Limits: defaultLimits(), ChurnWindow: 200,
		MinTokens: 60, Packages: reports})
	by := make(map[string]*narrate.Package, len(m.Packages))
	for i := range m.Packages {
		by[m.Packages[i].Path] = &m.Packages[i]
	}
	return m, by
}

func intp(v int) *int { return &v }

func TestPackageSentences(t *testing.T) {
	t.Parallel()

	_, by := narrated(
		row{path: "big", tier: tierPartition, passes: 7.8, days: 328.2, sloc: 3000, exports: 80, untested: 16, dup: 42.6,
			dupBlocks: 2, testFuncs: 1, hasTests: true, churn: intp(41), authors: 1, fanIn: 1, cogP90: 11, nest: 5,
			functions:  []details.Function{{Name: "Hard", File: "h.go", Line: 3, Cognitive: 56, Cyclomatic: 47}, {Name: "Easy", Cognitive: 1}},
			duplicates: []details.Span{{File: "a.go", StartLine: 1, EndLine: 81}, {File: "b.go", StartLine: 5, EndLine: 85}},
			specTokens: 680, volumeTokens: 300},
		row{path: "near", tier: tierFewPasses, passes: 2.4, days: 9.5, sloc: 1200, exports: 10, dup: 21, hasTests: true,
			functions: []details.Function{{Name: "A", Cognitive: 12}, {Name: "B", Cognitive: 3}}},
		row{path: "dupey", passes: 0.5, days: 1, sloc: 50, dup: 6, hasTests: true,
			duplicates: []details.Span{{File: "x.go", StartLine: 2, EndLine: 9}}},
		row{path: "bare", days: 0.2, sloc: 5, functions: []details.Function{{Name: "main", Cognitive: 0}}},
		row{path: "empty"},
	)
	tests := []struct {
		path, field, got, want string
	}{
		{"big", "story", by["big"].Story, "It is over 2 of its five limits (size, exports), and 42.6% of its lines are copies of each other, and 16 of its 80 exports have no test."},
		{"big", "todo", by["big"].Todo, "Extract the copied code first, then split the package along its responsibilities."},
		{"big", "changes", by["big"].Changes, "It changed 41 times in the last 200 commits, so every one of those problems is in the way of day-to-day work."},
		{"big", "rebuild", by["big"].Rebuild.Say, "Too big to rebuild as it stands: an agent would need about 8 sittings, so the package has to be split first. For one engineer, about 1 engineer-year."},
		{"big", "copies", by["big"].Copies.Say, "Nearly half of this code exists at least twice. A bug fixed in one copy stays alive in the others, and every copy is more for a rebuild to read."},
		{"big", "longest", by["big"].Copies.Longest, "Longest repeat: a.go:1-81 (81 lines), again at b.go:5-85."},
		{"big", "complexity", by["big"].Complexity.Say, "1 function is hard to follow: it branches and nests enough that a rewrite would likely get it wrong. Start with the ones below. The hardest tenth of functions score 11 or more; code nests up to 5 levels deep."},
		{"near", "story", by["near"].Story, "It is over its size limit, and 21% of its lines are copies of each other."},
		{"near", "todo", by["near"].Todo, "Plan a split now, before it grows further past its limit."},
		{"near", "rebuild", by["near"].Rebuild.Say, "An agent would need about 3 sittings, carrying notes between them. For one engineer, about 2 weeks."},
		{"near", "copies", by["near"].Copies.Say, "A large share of this code exists at least twice. A bug fixed in one copy stays alive in the others."},
		{"near", "complexity", by["near"].Complexity.Say, "Most functions are easy to follow; 1 needs some care. The hardest tenth of functions score 0 or more; code nests up to 0 levels deep."},
		{"near", "changes", by["near"].Changes, ""},
		{"dupey", "story", by["dupey"].Story, "It is inside every limit."},
		{"dupey", "todo", by["dupey"].Todo, "Nothing. Leave it alone."},
		{"dupey", "rebuild", by["dupey"].Rebuild.Say, "Small enough that an AI agent could rewrite it from its tests in one sitting. For one engineer, about 1 day."},
		{"dupey", "copies", by["dupey"].Copies.Say, "Some of this code is repeated. Worth extracting when you are next in here, not urgent."},
		{"dupey", "longest", by["dupey"].Copies.Longest, "Longest repeat: x.go:2-9 (8 lines)."},
		{"dupey", "complexity", by["dupey"].Complexity.Say, ""},
		{"bare", "story", by["bare"].Story, "It is inside every limit, and it has no tests at all."},
		{"bare", "copies", by["bare"].Copies.Say, "Very little repetition. Nothing to do."},
		{"bare", "longest", by["bare"].Copies.Longest, ""},
		{"bare", "complexity", by["bare"].Complexity.Say, "Every function here is easy to follow. The hardest tenth of functions score 0 or more; code nests up to 0 levels deep."},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s %s = %q, want %q", tt.path, tt.field, tt.got, tt.want)
		}
	}

	watch, _ := narrated(row{path: "w", tier: tierFewPasses, hasTests: true})
	if got, want := watch.Packages[0].Todo, "Nothing yet. Keep an eye on it."; got != want {
		t.Errorf("watch todo without a ceiling over = %q, want %q", got, want)
	}
	split, _ := narrated(row{path: "s", tier: tierPartition, hasTests: true})
	if got, want := split.Packages[0].Todo, "Split it into smaller packages before the next feature lands here."; got != want {
		t.Errorf("split todo without copies = %q, want %q", got, want)
	}
	copied, _ := narrated(row{path: "c", hasTests: true, dup: 56.9, churn: intp(1)}, row{path: "q", hasTests: true, churn: intp(0)},
		row{path: "self", hasTests: true, duplicates: []details.Span{{File: "f.go", StartLine: 171, EndLine: 267}, {File: "f.go", StartLine: 219, EndLine: 315}}})
	for _, tt := range []struct{ name, got, want string }{
		// A package watched for its copies alone is inside every limit:
		// "keep an eye on it" read as nothing to do on 56.9% copied code.
		{"copied todo", copied.Packages[0].Todo, "Extract the copied code next time you work here; nothing needs a split yet."},
		{"copied say", copied.Packages[0].Copies.Say, "Half or more of this code exists at least twice. A bug fixed in one copy stays alive in the others, and every copy is more for a rebuild to read."},
		{"changed once", copied.Packages[0].Changes, "It changed 1 time in the last 200 commits, so every one of those problems is in the way of day-to-day work."},
		{"never changed", copied.Packages[1].Changes, "It did not change in the last 200 commits."},
		{"overlapping repeat", copied.Packages[2].Copies.Longest, "Longest repeat: f.go:171-267 (97 lines), again at f.go:219-315, overlapping it: the same code back to back."},
	} {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
	all := report.Report{PackagePath: "all", Metrics: metrics.RawMetrics{SLOC: 2000, TokensEst: 20000, LargestFileSLOC: 700,
		ExportedSymbols: 70, InternalImports: 11, HasTests: true}}
	if got, want := pkg(&all, defaultLimits(), 200).Story, "It is over all five of its limits (size, tokens, largest file, exports, imports)."; got != want {
		t.Errorf("story over every limit = %q, want %q", got, want)
	}
	many, _ := narrated(row{path: "m", hasTests: true, functions: []details.Function{{Cognitive: 21}, {Cognitive: 30}, {Cognitive: 11}, {Cognitive: 12}}})
	if got, want := many.Packages[0].Complexity.Say, "2 functions are hard to follow: they branch and nest enough that a rewrite would likely get them wrong. Start with the ones below. The hardest tenth of functions score 0 or more; code nests up to 0 levels deep."; got != want {
		t.Errorf("complexity with two hard = %q, want %q", got, want)
	}
	care, _ := narrated(row{path: "c", hasTests: true, functions: []details.Function{{Cognitive: 11}, {Cognitive: 12}}})
	if got := care.Packages[0].Complexity.Say; !strings.HasPrefix(got, "Most functions are easy to follow; 2 need some care.") {
		t.Errorf("complexity with two needing care = %q", got)
	}

	wantFacts := []narrate.Fact{
		{Label: "Rebuild", Text: "7.8 agent passes · 328.2 human days"},
		{Label: "Copies", Text: "2 duplicate blocks cover 42.6% of lines"},
		{Label: "Complexity", Text: "cognitive p90 11 · cyclomatic p90 0 · nesting 5"},
		{Label: "Tests", Text: "1 test function · 16 of 80 exports untested"},
		{Label: "Changes", Text: "41 commits by 1 author in the last 200"},
		{Label: "Used by", Text: "1 internal package"},
	}
	if got := by["big"].Facts; !slices.Equal(got, wantFacts) {
		t.Errorf("facts = %q, want %q", got, wantFacts)
	}
	if got := by["near"].Facts; len(got) != 5 || got[4] != (narrate.Fact{Label: "Used by", Text: "0 internal packages"}) {
		t.Errorf("facts without churn = %q, want no Changes line", got)
	}
}

func TestModuleSentences(t *testing.T) {
	t.Parallel()

	m, _ := narrated(
		row{path: "hot", tier: tierPartition, passes: 4, days: 30, dup: 12.5, hasTests: true, churn: intp(9)},
		row{path: "cold", tier: tierPartition, passes: 9, days: 300, hasTests: true, churn: intp(0)},
		row{path: "fine", days: 2, hasTests: true, churn: intp(1)},
	)
	if want := "Shop has 2 packages that need a split. Start with hot."; m.Headline != want {
		t.Errorf("headline = %q, want %q", m.Headline, want)
	}
	if want := "3 packages; 2 need more than one agent sitting to rebuild from their tests. For one engineer the whole module is about 2 engineer-years of rebuild time. 1 is fine, 0 need watching, and 2 need a split. The hottest of those changed 9 times in the last 200 commits and 12.5% of it is copied code."; m.Lead != want {
		t.Errorf("lead = %q, want %q", m.Lead, want)
	}
	// A fine package is never a hotspot: "Start here" with "Nothing.
	// Leave it alone." told the reader to start on nothing.
	if want := []string{"hot", "cold"}; !slices.Equal(m.Hotspots, want) {
		t.Errorf("hotspots = %q, want %q", m.Hotspots, want)
	}
	// A split package only names the first hotspot when that is one.
	watchFirst, _ := narrated(row{path: "busy", tier: tierFewPasses, passes: 2, days: 30, hasTests: true, churn: intp(30)},
		row{path: "big", tier: tierPartition, passes: 8, days: 100, hasTests: true, churn: intp(1)})
	if want := "Shop has 1 package that needs a split."; watchFirst.Headline != want {
		t.Errorf("headline with a watched package first = %q, want %q", watchFirst.Headline, want)
	}
	if !strings.HasSuffix(watchFirst.Lead, " 0 are fine, 1 needs watching, and 1 needs a split. The hottest of those changed 1 time in the last 200 commits and 0% of it is copied code.") {
		t.Errorf("lead with one of each = %q", watchFirst.Lead)
	}

	quiet, _ := narrated(row{path: "a", days: 1, hasTests: true})
	if want := "Shop has no package that needs a split."; quiet.Headline != want {
		t.Errorf("headline = %q, want %q", quiet.Headline, want)
	}
	if want := "1 package; 0 need more than one agent sitting to rebuild from their tests. For one engineer the whole module is about 1 day of rebuild time. 1 is fine, 0 need watching, and 0 need a split."; quiet.Lead != want {
		t.Errorf("lead without a split = %q, want %q", quiet.Lead, want)
	}
	// Without churn the packages to start with are ranked by rebuild days,
	// and the lead says so; a fine package still never is one.
	unmeasured, _ := narrated(row{path: "s", tier: tierPartition, days: 1, hasTests: true},
		row{path: "w", tier: tierFewPasses, days: 5, hasTests: true}, row{path: "f", days: 50, hasTests: true})
	if strings.Contains(unmeasured.Lead, "hottest") {
		t.Errorf("lead without churn = %q, want no hottest sentence", unmeasured.Lead)
	}
	if want := " There is no change history to read, so the packages to start with are ranked by rebuild time alone."; !strings.HasSuffix(unmeasured.Lead, want) {
		t.Errorf("lead without churn = %q, want it to end %q", unmeasured.Lead, want)
	}
	if want := []string{"w", "s"}; !slices.Equal(unmeasured.Hotspots, want) {
		t.Errorf("hotspots without churn = %q, want %q", unmeasured.Hotspots, want)
	}

	terms := make([]string, 0, len(m.Glossary))
	for _, g := range m.Glossary {
		terms = append(terms, g.Term)
	}
	if want := []string{"Rebuild cost", "Split", "Watch", "Copied", "Untested", "Changes", "Limits", "Hard to follow"}; !slices.Equal(terms, want) {
		t.Errorf("glossary terms = %q, want %q", terms, want)
	}
	for _, g := range m.Glossary {
		switch g.Term {
		case "Limits":
			if want := "Per package: 1,000 lines, 16,000 tokens, a 600-line largest file, 60 exports, 10 internal imports. Fitted from well-regarded Go modules."; g.Text != want {
				t.Errorf("limits = %q, want %q", g.Text, want)
			}
		case "Copied":
			if !strings.Contains(g.Text, "duplicate blocks of 60 or more tokens") {
				t.Errorf("copied = %q, want the floor of 60", g.Text)
			}
		case "Changes":
			if !strings.HasPrefix(g.Text, "Commits touching the package in the last 200.") {
				t.Errorf("changes = %q, want the window of 200", g.Text)
			}
		}
	}
	bare := narrate.Glossary("typescript", nil, 0, 0, report.HumanBasis{})
	for _, g := range bare {
		if g.Term == "Limits" || g.Term == "Copied" || g.Term == "Changes" {
			t.Errorf("glossary without numbers has %s", g.Term)
		}
	}
}

func fp(v float64) *float64 { return &v }

func TestChangeSentences(t *testing.T) {
	t.Parallel()

	reports := []report.Report{row{path: "app", days: 12, hasTests: true, churn: intp(7)}.report(),
		row{path: "lib", hasTests: true}.report(), row{path: "ok", hasTests: true}.report(), row{path: "idle", hasTests: true}.report()}
	loc := &report.Location{File: "app/diag.go", Line: 266}
	app := report.Report{PackagePath: "app", Baseline: &report.Baseline{Ref: "bce3876a"},
		Violations: []report.Finding{
			{Metric: "dup_blocks", Base: fp(12), Head: 16, Limit: "max_delta +0; duplication_pct 4 -> 8, max_delta +6", Location: loc,
				Suggestion: "16 duplicate blocks cover 8% of lines; extract shared helpers. More."},
			{Metric: "sloc", Base: fp(3973), Head: 4389, Limit: "over max by 3389, allowed 150; tokens_est 1 -> 2, over max by 3, allowed 2000",
				Suggestion: "grew 416 while over the 1000 ceiling, more than the 150 one change may add there; split the package."},
			{Metric: "globals", Head: 1, Limit: "max 0"},
		},
		Warnings: []report.Finding{{Metric: "internal_imports", Base: fp(16), Head: 16, Limit: "max 10"}}}
	lib := report.Report{PackagePath: "lib", Violations: []report.Finding{{Metric: "exported_symbols", Base: fp(197), Head: 203, Limit: "over max by 143, allowed 5"}},
		Warnings: []report.Finding{{Metric: "internal_imports", Head: 11, Limit: "max 10"}, {Metric: "untested_exports", Base: fp(0), Head: 1}}}
	cross := 199
	baseCross := 185
	mod := report.Report{PackagePath: moduleRow, Metrics: metrics.RawMetrics{DupBlocksCrossPkg: &cross},
		Baseline: &report.Baseline{Metrics: metrics.RawMetrics{DupBlocksCrossPkg: &baseCross}}}
	c := &report.Check{Module: &report.CheckedPackage{Report: mod},
		Packages: []report.CheckedPackage{{Report: lib}, {Report: app}, {Report: report.Report{PackagePath: "ok"}}}}
	m := Build(&Input{ModulePath: "example.com/shop", Language: "go", Limits: defaultLimits(), ChurnWindow: 200,
		Base: "main", Packages: reports, Check: c})
	ch := m.Change
	if ch == nil {
		t.Fatal("change = nil, want a change section")
	}
	if ch.Verdict != "FAIL" || ch.Changed != 3 || ch.Failed != 2 || ch.Violations != 4 || ch.Warnings != 3 || ch.Passed != 1 ||
		ch.Unchanged != 1 || ch.BaseCommit != "bce3876a" || *ch.Cross.Base != 185 || ch.Cross.Head != 199 {
		t.Errorf("change counts = %+v", *ch)
	}
	if want := "2 packages need attention before this merges."; ch.Headline != want {
		t.Errorf("headline = %q, want %q", ch.Headline, want)
	}
	if want := "3 packages changed. 2 fail the gate with 4 findings. 1 passes clean."; ch.Lead != want {
		t.Errorf("lead = %q, want %q", ch.Lead, want)
	}
	if want := "1 changed package passes clean, 1 did not change"; ch.PassedSummary != want {
		t.Errorf("passed summary = %q, want %q", ch.PassedSummary, want)
	}
	if got := []string{ch.Failing[0].Path, ch.Failing[1].Path}; !slices.Equal(got, []string{"app", "lib"}) {
		t.Errorf("failing = %q, want app first, with more findings", got)
	}
	if want := "3 findings · 12 days to rebuild · 7 changes lately"; ch.Failing[0].Summary != want {
		t.Errorf("summary = %q, want %q", ch.Failing[0].Summary, want)
	}
	dup, sloc, globals := ch.Failing[0].Findings[0], ch.Failing[0].Findings[1], ch.Failing[0].Findings[2]
	tests := []struct{ name, got, want string }{
		{"dup why", dup.Why, "A new copy is a second place every future fix has to land."},
		{"dup do", dup.Do, "16 duplicate blocks cover 8% of lines."},
		{"dup moved", dup.Moved, "4 new duplicate blocks; the rule allows none"},
		{"dup moved short", dup.MovedShort, "4 new copies, none allowed"},
		// A change that crosses the ceiling was not "already past" it.
		{"sloc why", sloc.Why, "The package is past what one agent pass can hold, and this change took it there or grew it more than one change is allowed to."},
		{"sloc do", sloc.Do, "Grew 416 while over the 1000 ceiling, more than the 150 one change may add there."},
		{"sloc moved", sloc.Moved, "grew 416, allowed 150 while over the limit"},
		{"sloc moved short", sloc.MovedShort, "grew 416, allowed 150"},
		{"globals moved", globals.Moved, ""},
		{"globals why", globals.Why, "Shared mutable state is a dependency no signature shows, so callers and tests rely on it without saying so."},
		{"lib why", ch.Failing[1].Findings[0].Why, "A wider surface is more to reverse-engineer and more callers to keep working."},
		{"lib moved", ch.Failing[1].Findings[0].Moved, "grew 6, allowed 5 while over the limit"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
	if *sloc.OverBy != 3389 || *sloc.Allowed != 150 || dup.OverBy != nil || dup.Location != loc {
		t.Errorf("parsed limits: sloc %v/%v, dup %v, location %v", *sloc.OverBy, *sloc.Allowed, dup.OverBy, dup.Location)
	}
	if g := ch.WarningGroups[0]; g.Metric != "internal_imports" || len(g.Packages) != 2 || g.Why != "close to or over 10 internal imports" {
		t.Errorf("first warning group = %+v, want internal_imports on two packages", g)
	}
	if g := ch.WarningGroups[1]; g.Why != "new exports without a test" {
		t.Errorf("untested warning why = %q", g.Why)
	}

	// A package with no tests at all fails has_tests: the lead must not
	// say nothing new was left untested.
	bare := Build(&Input{Packages: reports, Check: &report.Check{
		Module: &report.CheckedPackage{Report: report.Report{PackagePath: moduleRow, Violations: []report.Finding{
			{Metric: "dup_blocks_cross_pkg", Base: fp(45), Head: 51, Limit: "max_delta +0"}}}},
		Packages: []report.CheckedPackage{{Report: report.Report{PackagePath: "lib",
			Violations: []report.Finding{{Metric: "has_tests", Base: fp(0), Head: 0, Limit: "require true"}}}}}}}).Change
	if want := "1 package changed. 2 fail the gate with 2 findings. 0 pass clean."; bare.Lead != want {
		t.Errorf("lead with a package without tests = %q, want %q", bare.Lead, want)
	}
	if f := bare.Failing[0].Findings[0]; f.Moved != "6 new duplicate blocks shared with other packages; the rule allows none" ||
		f.MovedShort != "6 new copies across packages, none allowed" {
		t.Errorf("cross moved = %q / %q", f.Moved, f.MovedShort)
	}

	pass := Build(&Input{Packages: reports, Check: &report.Check{Packages: []report.CheckedPackage{{Report: report.Report{PackagePath: "ok"}}}}}).Change
	if pass.Verdict != "PASS" || pass.Headline != "Nothing needs attention before this merges." ||
		pass.Lead != "1 package changed. 0 fail the gate with 0 findings. 1 passes clean, and nothing new was left untested." {
		t.Errorf("passing change = %q / %q / %q", pass.Verdict, pass.Headline, pass.Lead)
	}
}

func TestWords(t *testing.T) {
	t.Parallel()

	for metric, want := range map[string]string{"sloc": "size", "tokens_est": "tokens", "largest_file_sloc": "largest file",
		"exported_symbols": "exports", "internal_imports": "imports", "dup_blocks": "dup blocks"} {
		if got := metricLabel(metric); got != want {
			t.Errorf("metricLabel(%s) = %q, want %q", metric, got, want)
		}
	}
	for metric, want := range map[string]string{"changed_func_cognitive_max": "A function this complex is the one a rebuild gets wrong.",
		"largest_file_sloc": "One file is carrying the growth, and a rebuild has to read all of it."} {
		if got := why(metric); got != want {
			t.Errorf("why(%s) = %q, want %q", metric, got, want)
		}
	}
	for metric, want := range map[string]string{"sloc": "close to or over the size limit",
		"tokens_est": "close to or over the token limit for one agent pass", "largest_file_sloc": "a file close to or over 600 lines",
		"exported_symbols": "close to or over 60 exports", "globals": "new package-level state",
		"cognitive_p90": "complexity drifting up", "has_tests": ""} {
		if got := warnWhy(metric, defaultLimits()); got != want {
			t.Errorf("warnWhy(%s) = %q, want %q", metric, got, want)
		}
	}
	if got := warnWhy("internal_imports", nil); got != "" {
		t.Errorf("warnWhy without a ceiling = %q, want empty", got)
	}
	for in, want := range map[string]string{"a; b. c": "A.", "a. b; c": "A.", "only.": "Only.", "": "", "grew 3; split it": "Grew 3."} {
		if got := firstClause(in); got != want {
			t.Errorf("firstClause(%q) = %q, want %q", in, got, want)
		}
	}
	if got := failingSummary(1, nil); got != "1 finding" {
		t.Errorf("failingSummary of the module row = %q", got)
	}
	if got := countWord(7); got != "7" {
		t.Errorf("countWord(7) = %q", got)
	}
}
