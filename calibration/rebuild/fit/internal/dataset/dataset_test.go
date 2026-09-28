package dataset_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/dataset"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/model"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/synth"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/agent"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/runner"
)

// rows returns two packages' rows, three runs each, all passing.
func rows() []runner.RunRow {
	return synth.Rows(synth.RandomMetrics(2, 1), &synth.Plant{
		Params: model.Params{Overhead: 1000, Scale: 2, Budget: 25000, PerExport: 40, PerUntested: 800, PerHidden: 400, Exponent: 1.3},
		Runs:   3, Agent: "a", Model: "m", Seed: 1,
	})
}

func TestMeasure(t *testing.T) {
	u := agent.Usage{InputTokens: new(int64(1)), OutputTokens: new(int64(10)), CacheReadTokens: new(int64(100)),
		CacheWriteTokens: new(int64(1000))}
	for _, tt := range []struct {
		name string
		want float64
	}{{"footprint", 1011}, {"total", 1111}, {"output", 10}} {
		m, err := dataset.ParseMeasure(tt.name)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := m.Tokens(&u); !ok || got != tt.want {
			t.Errorf("%s = %v %v, want %v", tt.name, got, ok, tt.want)
		}
		if m.Formula() == "" {
			t.Errorf("%s has no formula", tt.name)
		}
	}
	if _, err := dataset.ParseMeasure("cost"); err == nil {
		t.Error("unknown measure parsed")
	}
	u.CacheReadTokens = nil
	if _, ok := dataset.Total.Tokens(&u); ok {
		t.Error("total without cache reads reported")
	}
	if got, ok := dataset.Footprint.Tokens(&u); !ok || got != 1011 {
		t.Errorf("footprint without cache reads = %v %v", got, ok)
	}
}

func TestBuild(t *testing.T) {
	rs := rows()
	// p0 run 2 fails at the turn cap; p1 run 3 changed a test file; p1 run
	// 2 passed but reported no tokens; p0 run 3 never got a verdict.
	for i := range rs {
		r := &rs[i]
		switch {
		case r.Package == "example.com/synth/p0" && r.Run == 2:
			r.Oracle.Passed, r.Oracle.TestsPass = false, false
			r.Measured.TurnCapHit, r.Measured.Turns = new(true), new(100)
		case r.Package == "example.com/synth/p1" && r.Run == 3:
			r.Valid = false
			r.Changes.TestFiles = []string{"x_test.go"}
		case r.Package == "example.com/synth/p1" && r.Run == 2:
			r.Measured.OutputTokens = nil
		case r.Package == "example.com/synth/p0" && r.Run == 3:
			r.Oracle.Completed = false
		}
	}
	s, err := dataset.Build(rs, dataset.Footprint)
	if err != nil {
		t.Fatal(err)
	}
	if s.Agent != "a" || s.Model != "m" || len(s.Models) != 1 || s.Rows != 6 {
		t.Errorf("set %s %s %v %d", s.Agent, s.Model, s.Models, s.Rows)
	}
	if len(s.Packages) != 2 || s.Packages[0].Runs != 2 || s.Packages[0].Passes != 1 || s.Packages[1].Runs != 1 {
		t.Fatalf("packages %+v", s.Packages)
	}
	if len(s.Censored) != 1 || !strings.Contains(s.Censored[0].Reason, "hit the turn cap of 100") || s.Censored[0].Turns != 100 {
		t.Errorf("censored %+v", s.Censored)
	}
	if len(s.Excluded) != 3 {
		t.Fatalf("excluded %+v", s.Excluded)
	}
	reasons := s.Excluded[0].Reason + s.Excluded[1].Reason + s.Excluded[2].Reason
	for _, want := range []string{"no token counts", "prompt's rules (changed 1 test files", "did not complete"} {
		if !strings.Contains(reasons, want) {
			t.Errorf("reasons %q lack %q", reasons, want)
		}
	}
	if len(s.Outcomes) != 3 || len(s.Measured()) != 2 || len(s.Unmeasured()) != 0 || s.CapHitPasses() != 0 {
		t.Errorf("outcomes %d measured %d unmeasured %d cap hits %d",
			len(s.Outcomes), len(s.Measured()), len(s.Unmeasured()), s.CapHitPasses())
	}

	// Every run of p0 failing leaves it unmeasured.
	rs = rows()
	for i := range rs {
		if rs[i].Package == "example.com/synth/p0" {
			rs[i].Oracle.Passed, rs[i].Oracle.BuildPasses = false, false
			rs[i].Agent.TimedOut = true
		}
	}
	s, err = dataset.Build(rs, dataset.Footprint)
	if err != nil {
		t.Fatal(err)
	}
	if un := s.Unmeasured(); len(un) != 1 || un[0].Package != "example.com/synth/p0" {
		t.Errorf("unmeasured %+v", un)
	}
	if !strings.Contains(s.Censored[0].Reason, "importers do not compile; timed out") {
		t.Errorf("reason %q", s.Censored[0].Reason)
	}
}

func TestBuildRefuses(t *testing.T) {
	tests := []struct {
		name string
		mut  func(rs []runner.RunRow) []runner.RunRow
		want string
	}{
		{"no rows", func([]runner.RunRow) []runner.RunRow { return nil }, "no rows"},
		{"two agents", func(rs []runner.RunRow) []runner.RunRow { rs[1].Agent.Name = "b"; return rs }, "one agent at a time"},
		{"two models", func(rs []runner.RunRow) []runner.RunRow { rs[1].Agent.Model = "n"; return rs }, "one agent at a time"},
		{"repeated run", func(rs []runner.RunRow) []runner.RunRow { return append(rs, rs[0]) }, "appears twice"},
		{"two units", func(rs []runner.RunRow) []runner.RunRow { rs[1].Unit = "tree"; return rs }, "fit one unit at a time"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := dataset.Build(tt.mut(rows()), dataset.Footprint); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for i, rs := range [][]runner.RunRow{rows()[:3], rows()[3:]} {
		var b strings.Builder
		for _, r := range rs {
			line, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			b.Write(line)
			b.WriteByte('\n')
		}
		p := filepath.Join(dir, "runs"+string(rune('a'+i))+".jsonl")
		if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	got, err := dataset.Load(paths)
	if err != nil || len(got) != 6 {
		t.Fatalf("Load = %d rows, %v", len(got), err)
	}
	empty := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(bad, []byte("{\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{empty, bad, filepath.Join(dir, "none.jsonl")} {
		if _, err := dataset.Load([]string{p}); err == nil {
			t.Errorf("%s loaded", filepath.Base(p))
		}
	}
}
