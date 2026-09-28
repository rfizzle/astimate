package main

import (
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
)

// TestSelectDefaults checks the select flags' defaults for each unit, and
// that an explicit flag wins.
func TestSelectDefaults(t *testing.T) {
	pkg, err := parseSelectFlags(io.Discard, []string{"--work", t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	want := definition.SelectionRule{PerStratum: 7, MaxPerModule: 3, MaxAgentPasses: 10}
	if pkg.rule != want || pkg.out != "calibration/rebuild/rebuild.yaml" || pkg.report != "calibration/rebuild/selection.md" {
		t.Fatalf("package defaults %+v", pkg)
	}
	tree, err := parseSelectFlags(io.Discard, []string{"--unit", "tree", "--max-per-module", "1", "--work", t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	want = definition.SelectionRule{PerStratum: 3, PerStratumUntested: 2, MaxPerModule: 1, MaxAgentPasses: 12,
		MinPackages: 2, MaxPackages: 8}
	if tree.rule != want || tree.out != "calibration/rebuild/rebuild-trees.yaml" || tree.report != "calibration/rebuild/selection-trees.md" {
		t.Fatalf("tree defaults %+v", tree)
	}
	if _, err := parseSelectFlags(io.Discard, []string{"--unit", "module"}); err == nil {
		t.Fatal("parseSelectFlags accepted --unit module")
	}
}

// TestDefaultOut checks that tree runs get their own default output
// directory, so their rows never share a file with package rows.
func TestDefaultOut(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	if got, want := defaultOut(definition.UnitPackage, "claude-code", now),
		filepath.Join("calibration", "data", "rebuild-2026-09-28-claude-code"); got != want {
		t.Errorf("package out = %s, want %s", got, want)
	}
	if got, want := defaultOut(definition.UnitTree, "claude-code", now),
		filepath.Join("calibration", "data", "rebuild-trees-2026-09-28-claude-code"); got != want {
		t.Errorf("tree out = %s, want %s", got, want)
	}
}
