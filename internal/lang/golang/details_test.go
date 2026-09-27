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
		})
	}
}

func TestDetailsUnknownPackage(t *testing.T) {
	e := New()
	mod := &metrics.ModuleContext{Root: fixtureRoot(t)}
	_, err := e.Details(t.Context(), mod, "example.com/fixture/absent")
	if !errors.Is(err, ErrUnknownPackage) {
		t.Errorf("Details error = %v, want it to wrap ErrUnknownPackage", err)
	}
}
