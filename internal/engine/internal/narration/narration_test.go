package narration

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/report"
)

// module returns a module named by its root only, with the default
// configuration.
func module(t *testing.T) *Module {
	t.Helper()
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	return &Module{Root: "/src/shop", Language: "go", Cfg: cfg, Version: "v0.0.0-test"}
}

// pkg returns a calibrated package report at dir of sloc lines.
func pkg(dir string, sloc int) report.Report {
	return report.Report{PackagePath: dir, Metrics: metrics.RawMetrics{SLOC: sloc, TokensEst: sloc * 12},
		Rebuild: report.Rebuild{Calibrated: true, AgentPasses: 1, HumanDays: 1, HumanDaysLow: 1, HumanDaysHigh: 2}}
}

func TestBuild(t *testing.T) {
	t.Parallel()

	m := module(t)
	got := Build(m, &Parts{Head: "abc123", Packages: []report.Report{pkg("b", 100), pkg("a", 50)}})
	if got.Name != "Shop" || got.Head != "abc123" || got.Language != "go" || got.AstimateVersion != "v0.0.0-test" {
		t.Errorf("module = %q %q %q %q, want Shop, abc123, go and the version", got.Name, got.Head, got.Language, got.AstimateVersion)
	}
	paths := make([]string, 0, len(got.Packages))
	for _, p := range got.Packages {
		paths = append(paths, p.Path)
	}
	if want := []string{"a", "b"}; !slices.Equal(paths, want) {
		t.Errorf("packages = %q, want %q", paths, want)
	}
	if got.Change != nil || len(got.Limits) != 5 || len(got.Glossary) == 0 {
		t.Errorf("module = %+v, want no change, five limits and a glossary", got)
	}
}

// TestCheckReport narrates a check's calibrated packages only, with the
// change against its base.
func TestCheckReport(t *testing.T) {
	t.Parallel()

	uncalibrated := pkg("raw", 10)
	uncalibrated.Rebuild.Calibrated = false
	c := &report.Check{Packages: []report.CheckedPackage{{Report: pkg("a", 50)}, {Report: uncalibrated}}}
	got := CheckReport(module(t), c, "master")
	if len(got.Packages) != 1 || got.Packages[0].Path != "a" {
		t.Errorf("packages = %+v, want a alone", got.Packages)
	}
	if got.Change == nil || got.Change.Base != "master" {
		t.Errorf("change = %+v, want one against master", got.Change)
	}
}

// TestSentences words a calibrated package as the module report does, and
// an uncalibrated one not at all.
func TestSentences(t *testing.T) {
	t.Parallel()

	m := module(t)
	big := pkg("big", 4000)
	story, todo := Sentences(m, &big)
	want := Build(m, &Parts{Packages: []report.Report{big}}).Packages[0]
	if story == "" || story != want.Story || todo != want.Todo {
		t.Errorf("Sentences = (%q, %q), want the report's (%q, %q)", story, todo, want.Story, want.Todo)
	}
	big.Rebuild.Calibrated = false
	if story, todo := Sentences(m, &big); story != "" || todo != "" {
		t.Errorf("uncalibrated Sentences = (%q, %q), want none", story, todo)
	}
}

// TestGlossary reads the embedded default configuration for a nil one.
func TestGlossary(t *testing.T) {
	t.Parallel()

	got := Glossary(nil, "go")
	want := Glossary(module(t).Cfg, "go")
	if len(got) == 0 || len(got) != len(want) {
		t.Fatalf("Glossary(nil) has %d terms, want the default's %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Glossary(nil)[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestCeilings(t *testing.T) {
	t.Parallel()

	maxOf := func(v float64) *float64 { return &v }
	ts := []gate.Threshold{
		{Metric: "internal_imports", Kind: gate.Capacity, Max: maxOf(10), Roles: []string{"library"}},
		{Metric: "dup_blocks", Kind: gate.Density, MaxDelta: maxOf(0)},
		{Metric: "sloc", Kind: gate.Capacity, Max: maxOf(1000)},
		{Metric: "sloc", Kind: gate.Capacity, Max: maxOf(2000)},
		// A density rule's max is no ceiling, nor is a capacity rule's
		// zero max: gate.Ceilings leaves both out, so the report does.
		{Metric: "tokens_est", Kind: gate.Density, Max: maxOf(16000), MaxDelta: maxOf(0)},
		{Metric: "largest_file_sloc", Kind: gate.Capacity, Max: maxOf(0)},
		{Metric: "internal_imports", Kind: gate.Capacity, Max: maxOf(25), Roles: []string{"main"}},
	}
	want := []narrate.Limit{
		{Metric: "sloc", Max: 1000}, {Metric: "sloc", Max: 2000},
		{Metric: "internal_imports", Max: 10, Roles: []string{"library"}},
		{Metric: "internal_imports", Max: 25, Roles: []string{"main"}},
	}
	got := ceilings(&config.Effective{Thresholds: ts})
	if len(got) != len(want) {
		t.Fatalf("ceilings = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Metric != want[i].Metric || got[i].Max != want[i].Max || !slices.Equal(got[i].Roles, want[i].Roles) {
			t.Errorf("ceilings[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	if got := ceilings(&config.Effective{Thresholds: cfg.Thresholds}); len(got) != 5 {
		t.Errorf("default ceilings = %+v, want five", got)
	}
}

// TestImportBoundary checks that narration reaches the module's packages
// only through the report's inputs and narrate, never an extractor, the
// gate, the estimate or a baseline: engine has already run those.
func TestImportBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	t.Parallel()

	allowed := []string{"internal/config", "internal/narrate", "internal/narrate/compose", "internal/narrate/markdown", "internal/report"}
	out, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Imports "\n"}}`, ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for imp := range strings.FieldsSeq(string(out)) {
		_, rel, ok := strings.Cut(imp, "/astimate/")
		if ok && !slices.Contains(allowed, rel) {
			t.Errorf("narration imports %s; it may import only %q from this module", imp, allowed)
		}
	}
}
