package narrate

import (
	"reflect"
	"testing"
)

// TestGroupSkipped checks that the packages not analysed are grouped by
// reason, the largest group first and equal groups by reason, with each
// group's paths sorted, and that nothing groups to nothing.
func TestGroupSkipped(t *testing.T) {
	t.Parallel()

	const constraints, missing, typeErr = "build constraints exclude all Go files", "missing module", "type error"
	tests := []struct {
		name string
		list []Skipped
		want []SkipGroup
	}{
		{name: "none", list: nil, want: nil},
		{
			name: "one reason",
			list: []Skipped{{Path: "tools/gen", Reason: constraints}, {Path: "cmd/x", Reason: constraints}},
			want: []SkipGroup{{Reason: constraints, Paths: []string{"cmd/x", "tools/gen"}}},
		},
		{
			name: "largest first, then by reason",
			list: []Skipped{
				{Path: "z", Reason: typeErr},
				{Path: "b", Reason: missing, Error: "no required module"},
				{Path: "c/gen", Reason: constraints},
				{Path: "a/gen", Reason: constraints},
			},
			want: []SkipGroup{
				{Reason: constraints, Paths: []string{"a/gen", "c/gen"}},
				{Reason: missing, Paths: []string{"b"}},
				{Reason: typeErr, Paths: []string{"z"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := GroupSkipped(tt.list); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GroupSkipped = %+v, want %+v", got, tt.want)
			}
		})
	}
}
