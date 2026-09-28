package golang

import (
	"context"
	"reflect"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

func TestModuleRow(t *testing.T) {
	var ext metrics.Extractor = New()
	mm, ok := ext.(metrics.ModuleMetrics)
	if !ok {
		t.Fatal("the Go extractor does not implement metrics.ModuleMetrics")
	}
	mod := &metrics.ModuleContext{Root: fixtureRoot(t)}
	row, err := mm.ModuleRow(t.Context(), mod)
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Validate(); err != nil {
		t.Errorf("module row: %v", err)
	}
	if row.DupBlocksCrossPkg == nil || *row.DupBlocksCrossPkg != 1 {
		t.Errorf("module dup_blocks_cross_pkg = %v, want 1", row.DupBlocksCrossPkg)
	}
	want := metrics.RawMetrics{DupBlocksCrossPkg: row.DupBlocksCrossPkg}
	if row != want {
		t.Errorf("module row = %+v, want only dup_blocks_cross_pkg set", row)
	}

	// The module row counts distinct blocks; the packages each count it.
	sum := 0
	for _, pkg := range fixturePackages() {
		m, err := ext.Extract(t.Context(), mod, pkg)
		if err != nil {
			t.Fatal(err)
		}
		sum += *m.DupBlocksCrossPkg
	}
	if sum != 2 {
		t.Errorf("sum of per-package dup_blocks_cross_pkg = %d, want 2", sum)
	}
}

// fixtureCrossBlock is the fixture's one cross-package block: a.Checksum
// and its copy b.Digest.
func fixtureCrossBlock() []metrics.CrossBlock {
	return []metrics.CrossBlock{{Occurrences: []metrics.Occurrence{
		{Package: "example.com/fixture/a", File: "a/a.go", StartLine: 7, EndLine: 19},
		{Package: "example.com/fixture/b", File: "b/b.go", StartLine: 13, EndLine: 25},
	}}}
}

// TestModuleDetails checks that the module row's details name every
// cross-package block of the fixture with files relative to the module
// root.
func TestModuleDetails(t *testing.T) {
	var ext metrics.Extractor = New()
	md, ok := ext.(metrics.ModuleDetailer)
	if !ok {
		t.Fatal("the Go extractor does not implement metrics.ModuleDetailer")
	}
	mod := &metrics.ModuleContext{Root: fixtureRoot(t)}
	got, err := md.ModuleDetails(t.Context(), mod)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.CrossBlocks, fixtureCrossBlock()) {
		t.Errorf("CrossBlocks = %+v, want %+v", got.CrossBlocks, fixtureCrossBlock())
	}
	// The result is a copy: changing it leaves the memoized pass intact.
	got.CrossBlocks[0].Occurrences[0].File = "changed"
	again, err := md.ModuleDetails(t.Context(), mod)
	if err != nil || !reflect.DeepEqual(again.CrossBlocks, fixtureCrossBlock()) {
		t.Errorf("second ModuleDetails = %+v, %v, want the fixture block unchanged", again.CrossBlocks, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := md.ModuleDetails(ctx, mod); err == nil {
		t.Error("ModuleDetails with a cancelled context returned no error")
	}
}

// TestDetailsCrossBlocks checks that a package's details list the
// cross-package blocks touching it, with every occurrence, and that a
// package no block touches lists none.
func TestDetailsCrossBlocks(t *testing.T) {
	e := New()
	mod := &metrics.ModuleContext{Root: fixtureRoot(t)}
	for _, pkg := range fixturePackages() {
		d, err := e.Details(t.Context(), mod, pkg)
		if err != nil {
			t.Fatalf("Details(%s): %v", pkg, err)
		}
		var want []metrics.CrossBlock
		if pkg == "example.com/fixture/a" || pkg == "example.com/fixture/b" {
			want = fixtureCrossBlock()
		}
		if !reflect.DeepEqual(d.CrossBlocks, want) {
			t.Errorf("%s: CrossBlocks = %+v, want %+v", pkg, d.CrossBlocks, want)
		}
	}
}

// TestCrossDuplicationStdlibNull checks that the standard-library loads,
// which are not modules, leave dup_blocks_cross_pkg null while still
// reporting the opacity flags.
func TestCrossDuplicationStdlibNull(t *testing.T) {
	m := extractStdlibOrSkip(t, "reflect")
	if m.DupBlocksCrossPkg != nil {
		t.Errorf("reflect: dup_blocks_cross_pkg = %d, want null", *m.DupBlocksCrossPkg)
	}
	if m.UsesReflect == nil || !*m.UsesReflect {
		t.Errorf("reflect: uses_reflect = %v, want true (it imports unsafe)", m.UsesReflect)
	}
}
