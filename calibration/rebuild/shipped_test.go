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
	d := loadShipped(t, "rebuild.yaml", "selection.md")
	if d.UnitOrDefault() != definition.UnitPackage {
		t.Fatalf("rebuild.yaml unit = %s", d.UnitOrDefault())
	}
	byPkg := sourceRows(t, d)
	for _, e := range d.Experiments {
		checkRow(t, byPkg, e.Package, e.Module, e.Commit, e.Metrics, e.AgentPasses, e.HumanDays)
	}
}

// TestShippedTreeDefinition checks that the committed rebuild-trees.yaml
// validates, which also checks each tree's sums and aggregate metrics, and
// that every member's metrics and estimate still match its row in the
// source data.
func TestShippedTreeDefinition(t *testing.T) {
	d := loadShipped(t, "rebuild-trees.yaml", "selection-trees.md")
	if d.UnitOrDefault() != definition.UnitTree {
		t.Fatalf("rebuild-trees.yaml unit = %s", d.UnitOrDefault())
	}
	byPkg := sourceRows(t, d)
	for _, e := range d.Experiments {
		for _, m := range e.Members {
			checkRow(t, byPkg, m.Package, e.Module, e.Commit, m.Metrics, m.AgentPasses, m.HumanDays)
		}
	}
}

// loadShipped loads and validates the definition at path and checks that
// its selection report exists.
func loadShipped(t *testing.T, path, report string) *definition.Definition {
	t.Helper()
	d, err := definition.LoadDefinition(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(report); err != nil {
		t.Errorf("selection report missing: %v", err)
	}
	return d
}

// sourceRows returns the rows of d's source, by import path.
func sourceRows(t *testing.T, d *definition.Definition) map[string]*selection.Row {
	t.Helper()
	rows, err := selection.ReadRows("../../" + d.Source)
	if err != nil {
		t.Fatal(err)
	}
	byPkg := make(map[string]*selection.Row, len(rows))
	for i := range rows {
		byPkg[rows[i].Package] = &rows[i]
	}
	return byPkg
}

// checkRow reports a package whose module, commit, metrics or estimate
// differ from its source row.
func checkRow(t *testing.T, byPkg map[string]*selection.Row, pkg, module, commit string, m definition.Metrics,
	passes, days float64) {
	t.Helper()
	r := byPkg[pkg]
	switch {
	case r == nil:
		t.Errorf("%s: not in the source", pkg)
	case r.Commit != commit || r.Module != module:
		t.Errorf("%s: module %s at %s, source has %s at %s", pkg, module, commit, r.Module, r.Commit)
	case !reflect.DeepEqual(r.Metrics, m.RawMetrics):
		t.Errorf("%s: metrics differ from the source row", pkg)
	case r.AgentPasses != passes || r.HumanDays != days:
		t.Errorf("%s: estimate %v/%v, source has %v/%v", pkg, passes, days, r.AgentPasses, r.HumanDays)
	}
}
