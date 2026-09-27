package golang

import (
	"errors"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

func TestDetailsDupes(t *testing.T) {
	const pkg = "example.com/fixture/dupes"
	want := metrics.Details{
		UntestedExports:  []string{"CountVisits", "SumOrders", "TallyScores"},
		UntestedExcluded: nil,
		DupLocations:     []string{"dupes.go:9-26", "dupes.go:31-48", "dupes.go:53-70"},
		UntestedPositions: []metrics.Position{
			{File: "dupes.go", Line: 53}, {File: "dupes.go", Line: 9}, {File: "dupes.go", Line: 31},
		},
		LargestFile: "dupes.go",
		SourceFiles: []string{"dupes.go"},
	}
	tests := []struct {
		name         string
		extractFirst bool
	}{
		{name: "after extract", extractFirst: true},
		{name: "extracts when nothing recorded", extractFirst: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := New()
			mod := &metrics.ModuleContext{Root: fixtureRoot(t)}
			if tt.extractFirst {
				if _, err := e.Extract(t.Context(), mod, pkg); err != nil {
					t.Fatalf("Extract(%s): %v", pkg, err)
				}
			}
			got, err := e.Details(t.Context(), mod, pkg)
			if err != nil {
				t.Fatalf("Details(%s): %v", pkg, err)
			}
			if !slices.Equal(got.UntestedExports, want.UntestedExports) {
				t.Errorf("UntestedExports = %q, want %q", got.UntestedExports, want.UntestedExports)
			}
			if len(got.UntestedExcluded) != 0 {
				t.Errorf("UntestedExcluded = %q, want none", got.UntestedExcluded)
			}
			if !slices.Equal(got.DupLocations, want.DupLocations) {
				t.Errorf("DupLocations = %q, want %q", got.DupLocations, want.DupLocations)
			}
			if !slices.Equal(got.UntestedPositions, want.UntestedPositions) {
				t.Errorf("UntestedPositions = %v, want %v", got.UntestedPositions, want.UntestedPositions)
			}
			if got.LargestFile != want.LargestFile {
				t.Errorf("LargestFile = %q, want %q", got.LargestFile, want.LargestFile)
			}
			if !slices.Equal(got.SourceFiles, want.SourceFiles) {
				t.Errorf("SourceFiles = %q, want %q", got.SourceFiles, want.SourceFiles)
			}
			if len(got.GlobalPositions) != 0 {
				t.Errorf("GlobalPositions = %v, want none", got.GlobalPositions)
			}
		})
	}
}

// TestDetailsPositions checks the declarations and files Details records
// for annotations on a package with globals and two source files.
func TestDetailsPositions(t *testing.T) {
	const pkg = "example.com/fixture/hidden"
	e := New()
	mod := &metrics.ModuleContext{Root: fixtureRoot(t)}
	got, err := e.Details(t.Context(), mod, pkg)
	if err != nil {
		t.Fatalf("Details(%s): %v", pkg, err)
	}
	wantGlobals := []metrics.Position{
		{File: "hidden.go", Line: 7}, {File: "hidden.go", Line: 10},
		{File: "hidden.go", Line: 11}, {File: "hidden.go", Line: 12},
	}
	if !slices.Equal(got.GlobalPositions, wantGlobals) {
		t.Errorf("GlobalPositions = %v, want %v", got.GlobalPositions, wantGlobals)
	}
	if want := []metrics.Position{{File: "hidden.go", Line: 20}}; !slices.Equal(got.UntestedPositions, want) {
		t.Errorf("UntestedPositions = %v, want %v (Drain)", got.UntestedPositions, want)
	}
	if got.LargestFile != "hidden.go" {
		t.Errorf("LargestFile = %q, want hidden.go", got.LargestFile)
	}
	if want := []string{"hidden.go", "setup.go"}; !slices.Equal(got.SourceFiles, want) {
		t.Errorf("SourceFiles = %q, want %q", got.SourceFiles, want)
	}
}

func TestDetailsUnknownPackage(t *testing.T) {
	e := New()
	mod := &metrics.ModuleContext{Root: fixtureRoot(t)}
	_, err := e.Details(t.Context(), mod, "example.com/fixture/absent")
	if !errors.Is(err, metrics.ErrUnknownPackage) {
		t.Errorf("Details error = %v, want it to wrap metrics.ErrUnknownPackage", err)
	}
}
