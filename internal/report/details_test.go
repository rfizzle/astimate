package report

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// relPkg maps a fixture import path to its module-relative directory.
func relPkg(pkg string) string { return strings.TrimPrefix(pkg, "example.com/app/") }

func TestNewDetails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   metrics.Details
		want *Details
	}{
		{name: "empty", in: metrics.Details{}, want: nil},
		{
			name: "source files only",
			in:   metrics.Details{SourceFiles: []string{"a.go"}},
			want: nil,
		},
		{
			name: "every field",
			in: metrics.Details{
				UntestedExports:   []string{"Parse", "T.Run"},
				UntestedExcluded:  []string{"Debug"},
				UntestedPositions: []metrics.Position{{File: "p.go", Line: 3}, {File: "t.go", Line: 9}},
				DupLocations:      []string{"a.go:4-20", "sub/b.go:30-46", "bad", "c.go:x-1"},
				GlobalPositions:   []metrics.Position{{File: "state.go", Line: 7}},
				LargestFile:       "a.go",
				SourceFiles:       []string{"a.go", "p.go"},
				CrossBlocks: []metrics.CrossBlock{{Occurrences: []metrics.Occurrence{
					{Package: "example.com/app/x", File: "x/x.go", StartLine: 1, EndLine: 9},
					{Package: "example.com/app/y", File: "y/y.go", StartLine: 5, EndLine: 13},
				}}},
			},
			want: &Details{
				Duplicates:       []Span{{File: "a.go", StartLine: 4, EndLine: 20}, {File: "sub/b.go", StartLine: 30, EndLine: 46}},
				UntestedExports:  []Declaration{{Name: "Parse", File: "p.go", Line: 3}, {Name: "T.Run", File: "t.go", Line: 9}},
				ExcludedUntested: []string{"Debug"},
				Globals:          []Location{{File: "state.go", Line: 7}},
				LargestFile:      "a.go",
				CrossBlocks: []CrossBlock{{Occurrences: []CrossOccurrence{
					{Package: "x", File: "x/x.go", StartLine: 1, EndLine: 9},
					{Package: "y", File: "y/y.go", StartLine: 5, EndLine: 13},
				}}},
			},
		},
		{
			name: "names without positions",
			in:   metrics.Details{UntestedExports: []string{"Parse"}},
			want: &Details{UntestedExports: []Declaration{{Name: "Parse"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NewDetails(&tt.in, relPkg); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewDetails = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDetailsJSONOmitsEmpty(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(Details{UntestedExports: []Declaration{{Name: "Parse"}}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"untested_exports":[{"name":"Parse"}]}`; string(data) != want {
		t.Errorf("json = %s, want %s", data, want)
	}
}
