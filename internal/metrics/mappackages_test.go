package metrics

import (
	"reflect"
	"strings"
	"testing"
)

func TestMapPackages(t *testing.T) {
	t.Parallel()

	in := []CrossBlock{{Occurrences: []Occurrence{
		{Package: "example.com/m/a", File: "a/a.go", StartLine: 1, EndLine: 9},
		{Package: "example.com/m/b", File: "b/b.go", StartLine: 3, EndLine: 11},
	}}}
	got := MapPackages(in, func(p string) string { return strings.TrimPrefix(p, "example.com/m/") })
	want := []CrossBlock{{Occurrences: []Occurrence{
		{Package: "a", File: "a/a.go", StartLine: 1, EndLine: 9},
		{Package: "b", File: "b/b.go", StartLine: 3, EndLine: 11},
	}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MapPackages = %+v, want %+v", got, want)
	}
	if in[0].Occurrences[0].Package != "example.com/m/a" {
		t.Error("MapPackages changed its input")
	}
	if MapPackages(nil, strings.ToUpper) != nil {
		t.Error("MapPackages(nil) is not nil")
	}
}
