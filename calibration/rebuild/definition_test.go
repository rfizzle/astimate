package main

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// validDefinition returns a definition that passes Validate: 36
// experiments, six per stratum.
func validDefinition() *Definition {
	d := &Definition{
		Source:        "calibration/data/2026-09-28-corpus/packages.jsonl",
		ConfigVersion: "default-uncalibrated-1",
		GoVersion:     "go1.27.1",
		Env:           defaultEnv(),
		Selection:     SelectionRule{PerStratum: 6, MaxPerModule: 2, MaxAgentPasses: 10},
	}
	passes := map[score.Tier]float64{score.TierOnePass: 0.5, score.TierFewPasses: 2, score.TierPartition: 5}
	for i := range 36 {
		s := strata()[i%6]
		dir := fmt.Sprintf("pkg%02d", i)
		oracle := Oracle{Test: []string{"./" + dir}, Build: []string{"./..."}}
		m := metrics.RawMetrics{Files: 1, SLOC: 10, LargestFileSLOC: 10, TokensEst: 100, TokensEstWithTests: 100, FuncCount: 1,
			UsesCgo: new(false), GeneratedFiles: new(0)}
		if s.tested {
			m.TestFuncs, m.HasTests, m.TestFiles, m.TokensEstWithTests = 1, true, 1, 150
		} else {
			oracle.Test = []string{"./user"}
		}
		d.Experiments = append(d.Experiments, Experiment{
			Module:        "example.com/mod",
			Repo:          "https://example.com/mod.git",
			Commit:        strings.Repeat("a", 40),
			Package:       "example.com/mod/" + dir,
			Dir:           dir,
			Stub:          StubSignatures,
			StubSHA256:    strings.Repeat("b", 64),
			Oracle:        oracle,
			TurnCap:       100,
			HasTests:      s.tested,
			Tier:          s.tier,
			AgentPasses:   passes[s.tier],
			RebuildTokens: 1000,
			HumanDays:     1,
			Metrics:       Metrics{m},
		})
	}
	return d
}

func TestDefinitionValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(d *Definition)
		want   string
	}{
		{"valid", func(*Definition) {}, ""},
		{"too few", func(d *Definition) { d.Experiments = d.Experiments[:29] }, "at least 30"},
		{"missing tier", func(d *Definition) {
			for i := range d.Experiments {
				if d.Experiments[i].Tier == score.TierPartition {
					d.Experiments[i].Tier = score.TierFewPasses
				}
			}
		}, "no PARTITION tested experiment"},
		{"tier without untested", func(d *Definition) {
			for i := range d.Experiments {
				e := &d.Experiments[i]
				if e.Tier == score.TierFewPasses {
					e.HasTests, e.Metrics.HasTests, e.Metrics.TestFuncs = true, true, 1
					e.Oracle.Test = []string{"./" + e.Dir}
				}
			}
		}, "no FEW_PASSES untested experiment"},
		{"duplicate package", func(d *Definition) { d.Experiments[1] = d.Experiments[0] }, "duplicate package"},
		{"std", func(d *Definition) { d.Experiments[0].Module = "std" }, "cloned corpus module"},
		{"dir mismatch", func(d *Definition) { d.Experiments[0].Dir = "other" }, "does not match the package path"},
		{"short commit", func(d *Definition) { d.Experiments[0].Commit = "abc" }, "40-character"},
		{"unknown stub", func(d *Definition) { d.Experiments[0].Stub = "delete" }, `stub "delete"`},
		{"bad stub hash", func(d *Definition) { d.Experiments[0].StubSHA256 = "" }, "stub_sha256"},
		{"no turn cap", func(d *Definition) { d.Experiments[0].TurnCap = 0 }, "turn_cap"},
		{"bad tier", func(d *Definition) { d.Experiments[0].Tier = "HUGE" }, "tier \"HUGE\""},
		{"tested oracle elsewhere", func(d *Definition) { d.Experiments[0].Oracle.Test = []string{"./other"} }, "must be [./pkg00]"},
		{"untested oracle own", func(d *Definition) { d.Experiments[1].Oracle.Test = []string{"./pkg01"} }, "must list its importers"},
		{"oracle absolute", func(d *Definition) { d.Experiments[1].Oracle.Test = []string{"example.com/mod/user"} }, "not module-relative"},
		{"oracle build", func(d *Definition) { d.Experiments[0].Oracle.Build = []string{"."} }, "oracle.build"},
		{"has_tests disagrees", func(d *Definition) { d.Experiments[0].Metrics.HasTests = false }, "disagrees"},
		{"bad metrics", func(d *Definition) { d.Experiments[0].Metrics.SLOC = -1 }, "sloc is negative"},
		{"cgo", func(d *Definition) { d.Experiments[0].Metrics.UsesCgo = new(true) }, "uses_cgo must be false"},
		{"cgo unknown", func(d *Definition) { d.Experiments[0].Metrics.UsesCgo = nil }, "uses_cgo must be false"},
		{"no go version", func(d *Definition) { d.GoVersion = "" }, "go_version"},
		{"bad env", func(d *Definition) { d.Env = []string{"CGO_ENABLED"} }, "KEY=VALUE"},
		{"no source", func(d *Definition) { d.Source = "" }, "source is required"},
		{"no rule", func(d *Definition) { d.Selection = SelectionRule{} }, "selection"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := validDefinition()
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
			if !errors.Is(err, errInvalidDefinition) {
				t.Fatalf("Validate() = %v, want it to wrap errInvalidDefinition", err)
			}
		})
	}
}

func TestDefinitionRoundTrip(t *testing.T) {
	d := validDefinition()
	cov := 42.5
	d.Experiments[0].Metrics.CoveragePct = &cov
	data, err := d.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\n      tokens_est_with_tests: 150\n") {
		t.Fatalf("metrics are not written as a block mapping under their JSON names:\n%s", data)
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
	if p := got.Experiments[0].Metrics.CoveragePct; p == nil || *p != cov {
		t.Fatalf("coverage_pct = %v, want %v", p, cov)
	}
}

func TestParseDefinitionRejects(t *testing.T) {
	data, err := validDefinition().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{"empty", "", "empty document"},
		{"unknown key", string(data) + "extra: 1\n", "field extra not found"},
		{"unknown metric", strings.Replace(string(data), "sloc: 10", "slocs: 10", 1), "unknown field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDefinition([]byte(tt.doc))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseDefinition() = %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

// TestShippedDefinition checks that the committed rebuild.yaml validates
// and that every experiment's metrics and estimate still match its row in
// the source data.
func TestShippedDefinition(t *testing.T) {
	d, err := LoadDefinition("rebuild.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ReadRows("../../" + d.Source)
	if err != nil {
		t.Fatal(err)
	}
	byPkg := make(map[string]*Row, len(rows))
	for i := range rows {
		byPkg[rows[i].Package] = &rows[i]
	}
	for _, e := range d.Experiments {
		r := byPkg[e.Package]
		switch {
		case r == nil:
			t.Errorf("%s: not in %s", e.Package, d.Source)
		case r.Commit != e.Commit || r.Module != e.Module:
			t.Errorf("%s: module %s at %s, source has %s at %s", e.Package, e.Module, e.Commit, r.Module, r.Commit)
		case !reflect.DeepEqual(r.Metrics, e.Metrics.RawMetrics):
			t.Errorf("%s: metrics differ from the source row", e.Package)
		case r.AgentPasses != e.AgentPasses || r.HumanDays != e.HumanDays:
			t.Errorf("%s: estimate %v/%v, source has %v/%v", e.Package, e.AgentPasses, e.HumanDays, r.AgentPasses, r.HumanDays)
		}
	}
	if _, err := os.Stat("selection.md"); err != nil {
		t.Errorf("selection report missing: %v", err)
	}
}
