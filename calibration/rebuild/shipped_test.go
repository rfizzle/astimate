package main

import (
	"os"
	"reflect"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/selection"
)

// TestShippedDefinition checks that the committed rebuild.yaml validates
// and that every experiment's metrics and estimate still match its row in
// the source data.
func TestShippedDefinition(t *testing.T) {
	d, err := definition.LoadDefinition("rebuild.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := selection.ReadRows("../../" + d.Source)
	if err != nil {
		t.Fatal(err)
	}
	byPkg := make(map[string]*selection.Row, len(rows))
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
