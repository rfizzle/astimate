package synth_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/dataset"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/model"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/synth"
)

func TestRows(t *testing.T) {
	pkgs := synth.RandomMetrics(10, 1)
	p := &synth.Plant{
		Params: model.Params{Overhead: 5000, Scale: 2, Budget: 25000, PerExport: 40, PerUntested: 800, PerHidden: 400, Exponent: 1.3},
		Runs:   3, FailEvery: 4, Agent: "a", Model: "m", Seed: 1,
	}
	rows := synth.Rows(pkgs, p)
	if len(rows) != 30 {
		t.Fatalf("%d rows, want 30", len(rows))
	}
	failed, capped := 0, 0
	for i, r := range rows {
		if r.Run != 1+i/10 || r.Metrics.TokensEst != pkgs[i%10].TokensEst {
			t.Fatalf("row %d is run %d of %s, want run-major order", i, r.Run, r.Package)
		}
		tokens, ok := dataset.Footprint.Tokens(&r.Measured.Usage)
		want := p.Params.Measured(model.InputsOf(&r.Metrics))
		if !ok || math.Abs(tokens-want) > 1 {
			t.Errorf("row %d footprint %v, want the planted %v", i, tokens, want)
		}
		if !r.Oracle.Passed {
			failed++
		}
		if *r.Measured.TurnCapHit {
			capped++
		}
		if !r.Valid || !r.Oracle.Completed || r.Agent.Name != "a" || r.Agent.Model != "m" {
			t.Errorf("row %d: %+v", i, r)
		}
	}
	if failed != 7 || capped != 3 {
		t.Errorf("%d failed, %d at the cap; want 7 and 3", failed, capped)
	}
}

func TestRandomMetrics(t *testing.T) {
	a, b := synth.RandomMetrics(100, 7), synth.RandomMetrics(100, 7)
	if !reflect.DeepEqual(a, b) {
		t.Error("the same seed drew different packages")
	}
	for i, m := range a {
		if m.TokensEst < 200 || m.TokensEst > 60000 || m.UntestedExports > m.ExportedSymbols ||
			m.TokensEstWithTests < m.TokensEst || m.HasTests != (m.TestFuncs > 0) {
			t.Errorf("package %d: %+v", i, m)
		}
		if err := m.Validate(); err != nil {
			t.Errorf("package %d does not validate: %v", i, err)
		}
	}
}
