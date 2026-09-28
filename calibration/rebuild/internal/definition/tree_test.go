package definition

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// validTreeDefinition returns a tree definition that passes Validate: 12
// trees of two packages each over six modules, eight of them tested.
func validTreeDefinition() *Definition {
	d := &Definition{
		Source:         "calibration/data/2026-09-28-corpus/packages.jsonl",
		ConfigVersion:  "thresholds-2026-09-27",
		GoVersion:      "go1.27.1",
		Env:            []string{"CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOWORK=off"},
		Unit:           UnitTree,
		EstimateMethod: EstimateSum,
		Selection: SelectionRule{PerStratum: 3, PerStratumUntested: 2, MaxPerModule: 2, MaxAgentPasses: 12,
			MinPackages: 2, MaxPackages: 8},
	}
	for i := range 12 {
		mod := fmt.Sprintf("example.com/mod%d", i/2)
		dir := fmt.Sprintf("tree%02d", i)
		tested := i < 8
		var members []Member
		for _, sub := range []string{dir, dir + "/sub"} {
			m := metrics.RawMetrics{Files: 1, SLOC: 10, LargestFileSLOC: 10, TokensEst: 100, TokensEstWithTests: 100,
				FuncCount: 1, DuplicationPct: 10, UsesCgo: new(false), UsesReflect: new(false), GeneratedFiles: new(0)}
			if tested {
				m.TestFuncs, m.HasTests, m.TestFiles, m.TokensEstWithTests = 1, true, 1, 150
			}
			members = append(members, Member{Package: mod + "/" + sub, Dir: sub, HasTests: tested, Tier: score.TierOnePass,
				AgentPasses: 0.1, RebuildTokens: 1000, HumanDays: 0.55, Metrics: Metrics{m}})
		}
		oracle := Oracle{Test: []string{TreePattern(dir)}, Build: []string{"./..."}}
		if !tested {
			oracle.Test = []string{"./user"}
		}
		d.Experiments = append(d.Experiments, Experiment{
			Module: mod, Repo: "https://example.com/mod.git", Commit: strings.Repeat("a", 40),
			Package: mod + "/" + dir, Dir: dir, Stub: StubSignatures, StubSHA256: strings.Repeat("b", 64),
			Oracle: oracle, TurnCap: 100, HasTests: tested, Tier: score.TierOnePass, AgentPasses: 0.1,
			RebuildTokens: 2000, HumanDays: 1.1, Metrics: Metrics{Aggregate(members)}, Members: members,
		})
	}
	return d
}

func TestTreeDefinitionValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(d *Definition)
		want   string
	}{
		{"valid", func(*Definition) {}, ""},
		{"too few", func(d *Definition) { d.Experiments = d.Experiments[:11] }, "want 12 to 16 trees"},
		{"too many", func(d *Definition) {
			for i := range 5 {
				e := d.Experiments[0]
				e.Package += fmt.Sprint(i)
				e.Module = fmt.Sprintf("example.com/extra%d", i)
				e.Package, e.Dir = e.Module+"/tree00", "tree00"
				d.Experiments = append(d.Experiments, e)
			}
		}, "want 12 to 16 trees"},
		{"no method", func(d *Definition) { d.EstimateMethod = "" }, "estimate_method"},
		{"unknown unit", func(d *Definition) { d.Unit = "module" }, `unit "module"`},
		{"no bounds", func(d *Definition) { d.Selection.MinPackages = 0 }, "min_packages"},
		{"mostly untested", func(d *Definition) {
			for i := range 6 {
				e := &d.Experiments[i]
				e.HasTests, e.Metrics.HasTests = false, false
			}
		}, "want at least half"},
		{"module over cap", func(d *Definition) {
			d.Experiments[2].Module = "example.com/mod0"
			d.Experiments[2].Package = "example.com/mod0/tree02"
			for i := range d.Experiments[2].Members {
				m := &d.Experiments[2].Members[i]
				m.Package = "example.com/mod0/" + m.Dir
			}
		}, "more than max_per_module"},
		{"one member", func(d *Definition) {
			e := &d.Experiments[0]
			e.Members = e.Members[:1]
			e.RebuildTokens, e.HumanDays, e.Metrics = 1000, 0.6, Metrics{Aggregate(e.Members)}
		}, "want 2 to 8 members"},
		{"first member", func(d *Definition) {
			e := &d.Experiments[0]
			e.Members[0], e.Members[1] = e.Members[1], e.Members[0]
		}, "first member"},
		{"member outside", func(d *Definition) {
			m := &d.Experiments[0].Members[1]
			m.Dir, m.Package = "other", "example.com/mod0/other"
		}, "is not below"},
		{"token sum", func(d *Definition) { d.Experiments[0].RebuildTokens = 1999 }, "members' sum 2000"},
		{"days sum", func(d *Definition) { d.Experiments[0].HumanDays = 2 }, "human_days"},
		{"aggregate", func(d *Definition) { d.Experiments[0].Metrics.SLOC = 21 }, "aggregate"},
		{"tested oracle", func(d *Definition) { d.Experiments[0].Oracle.Test = []string{"./tree00"} }, "must be [./tree00/...]"},
		{"untested oracle inside", func(d *Definition) { d.Experiments[8].Oracle.Test = []string{"./tree08/sub"} }, "outside it"},
		{"root tree", func(d *Definition) { d.Experiments[0].Dir = "." }, "whole module"},
		{"package members", func(d *Definition) {
			d.Unit, d.EstimateMethod = "", ""
		}, "members are for unit tree only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := validTreeDefinition()
			tt.mutate(d)
			err := d.Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() = %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

// TestTreeDefinitionRoundTrip checks that a tree definition, with its
// unit, estimate method, tree bounds and members, survives Marshal and
// ParseDefinition unchanged, and that a package definition still writes
// none of the tree fields.
func TestTreeDefinitionRoundTrip(t *testing.T) {
	data, err := validTreeDefinition().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"\nunit: tree\n", "\nestimate_method: sum\n", "\n  per_stratum_untested: 2\n",
		"\n  min_packages: 2\n", "\n    members:\n      - package: example.com/mod0/tree00\n        dir: tree00\n",
		"\n          tokens_est_with_tests: 150\n"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("tree definition lacks %q:\n%s", want, data)
		}
	}
	got, err := ParseDefinition(data)
	if err != nil {
		t.Fatalf("ParseDefinition: %v", err)
	}
	again, err := got.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) {
		t.Fatalf("round trip changed the document:\n%s\nvs\n%s", data, again)
	}
	if got.UnitOrDefault() != UnitTree || len(got.Experiments[3].Members) != 2 || got.Experiments[3].Members[1].Dir != "tree03/sub" {
		t.Fatalf("parsed tree definition %+v", got.Experiments[3])
	}

	pkg, err := validDefinition().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"unit:", "estimate_method:", "members:", "per_stratum_untested:", "min_packages:"} {
		if strings.Contains(string(pkg), field) {
			t.Fatalf("package definition writes %s:\n%s", field, pkg)
		}
	}
	if d, err := ParseDefinition(pkg); err != nil || d.UnitOrDefault() != UnitPackage {
		t.Fatalf("package definition parses as %v, %v", d, err)
	}
}

// TestTreeHelpers checks InTree, TreePattern and SelectionRule.Slots.
func TestTreeHelpers(t *testing.T) {
	for _, tt := range []struct {
		dir, p string
		want   bool
	}{{"a/b", "a/b", true}, {"a/b", "a/b/c", true}, {"a/b", "a/bc", false}, {"a/b", "a", false}} {
		if got := InTree(tt.dir, tt.p); got != tt.want {
			t.Errorf("InTree(%q, %q) = %v, want %v", tt.dir, tt.p, got, tt.want)
		}
	}
	if got := TreePattern("a/b"); got != "./a/b/..." {
		t.Errorf("TreePattern = %q", got)
	}
	r := SelectionRule{PerStratum: 3, PerStratumUntested: 2}
	if r.Slots(Stratum{Tested: true}) != 3 || r.Slots(Stratum{}) != 2 {
		t.Error("Slots ignores per_stratum_untested")
	}
	if r.PerStratumUntested = 0; r.Slots(Stratum{}) != 3 {
		t.Error("Slots without per_stratum_untested is not per_stratum")
	}
}

// TestAggregate checks that the aggregate sums counts, takes the largest
// of the maxima, weights duplication by tokens so the volume term sums, and
// nulls the ratios.
func TestAggregate(t *testing.T) {
	a := metrics.RawMetrics{Files: 1, SLOC: 10, LargestFileSLOC: 10, TokensEst: 1000, TokensEstWithTests: 1500,
		DuplicationPct: 10, MaxNesting: 2, CognitiveP90: 4, ExportedSymbols: 3, Instability: new(0.5),
		UsesCgo: new(false), GeneratedFiles: new(0)}
	b := metrics.RawMetrics{Files: 2, SLOC: 30, LargestFileSLOC: 20, TokensEst: 3000, TokensEstWithTests: 3000,
		DuplicationPct: 20, MaxNesting: 1, CognitiveP90: 7, ExportedSymbols: 5, HasTests: true, UsesReflect: new(true),
		UsesCgo: new(false), GeneratedFiles: new(0)}
	got := Aggregate([]Member{{Metrics: Metrics{a}}, {Metrics: Metrics{b}}})
	vol := 1000*0.9 + 3000*0.8
	switch {
	case got.Files != 3 || got.SLOC != 40 || got.TokensEst != 4000 || got.TokensEstWithTests != 4500 || got.ExportedSymbols != 8:
		t.Errorf("sums: %+v", got)
	case got.LargestFileSLOC != 20 || got.MaxNesting != 2 || got.CognitiveP90 != 7:
		t.Errorf("maxima: %+v", got)
	case fmt.Sprintf("%.6f", float64(got.TokensEst)*(1-got.DuplicationPct/100)) != fmt.Sprintf("%.6f", vol):
		t.Errorf("duplication_pct %v does not keep the volume %v", got.DuplicationPct, vol)
	case !got.HasTests || !*got.UsesReflect || *got.UsesCgo || *got.GeneratedFiles != 0 || got.Instability != nil:
		t.Errorf("flags and ratios: %+v", got)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("aggregate does not validate: %v", err)
	}
}
