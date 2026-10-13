package metrics

import (
	"maps"
	"slices"
	"strconv"
	"testing"
)

func TestChangedFunctions(t *testing.T) {
	fn := func(recv, name string, fp uint64, cog int) FunctionInfo {
		return FunctionInfo{Receiver: recv, Name: name, Fingerprint: fp, Cognitive: cog}
	}
	base := []FunctionInfo{
		fn("", "Parse", 1, 5),
		fn("T", "Run", 2, 7),
		fn("", "init", 3, 0),
		fn("", "init", 4, 1),
		fn("", "Gone", 5, 9),
	}
	tests := []struct {
		name string
		head []FunctionInfo
		want []string
	}{
		{"identical", slices.Clone(base), nil},
		{"body edited", []FunctionInfo{fn("", "Parse", 9, 6), fn("T", "Run", 2, 7)}, []string{"Parse"}},
		{"renamed", []FunctionInfo{fn("", "ParseAll", 1, 5)}, []string{"ParseAll"}},
		{"receiver changed", []FunctionInfo{fn("U", "Run", 2, 7)}, []string{"U.Run"}},
		{"method became function", []FunctionInfo{fn("", "Run", 2, 7)}, []string{"Run"}},
		{"added", []FunctionInfo{fn("", "Parse", 1, 5), fn("", "New", 7, 40)}, []string{"New"}},
		{"deleted is not changed", []FunctionInfo{fn("", "Parse", 1, 5)}, nil},
		{"repeated names match as a multiset", []FunctionInfo{fn("", "init", 4, 1), fn("", "init", 3, 0)}, nil},
		{"third init is new", []FunctionInfo{fn("", "init", 3, 0), fn("", "init", 4, 1), fn("", "init", 3, 0)}, []string{"init"}},
		{"empty head", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, f := range ChangedFunctions(base, tt.head) {
				got = append(got, f.QualifiedName())
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("ChangedFunctions = %q, want %q", got, tt.want)
			}
		})
	}
	t.Run("empty base", func(t *testing.T) {
		head := []FunctionInfo{fn("", "a", 1, 2), fn("T", "b", 2, 3)}
		if got := ChangedFunctions(nil, head); len(got) != 2 {
			t.Errorf("ChangedFunctions(nil, head) = %+v, want both functions", got)
		}
	})
}

func TestChangedFunctionsWithBase(t *testing.T) {
	fn := func(recv, name string, fp uint64, cog int) FunctionInfo {
		return FunctionInfo{Receiver: recv, Name: name, Fingerprint: fp, Cognitive: cog}
	}
	base := []FunctionInfo{
		fn("", "Parse", 1, 57),
		fn("UI", "Update", 2, 369),
		fn("", "init", 3, 4),
		fn("", "init", 4, 9),
	}
	// none is a changed function with no baseline value.
	const none = -1
	tests := []struct {
		name string
		head []FunctionInfo
		want map[string]int // qualified name -> base cognitive, or none
	}{
		{"unchanged", slices.Clone(base), map[string]int{}},
		{"modified keeps its own base", []FunctionInfo{fn("", "Parse", 9, 57), fn("UI", "Update", 7, 385)},
			map[string]int{"Parse": 57, "UI.Update": 369}},
		{"new has none", []FunctionInfo{fn("", "Parse", 1, 57), fn("", "New", 8, 60)}, map[string]int{"New": none}},
		{"renamed has none", []FunctionInfo{fn("", "ParseAll", 1, 57)}, map[string]int{"ParseAll": none}},
		{"receiver changed has none", []FunctionInfo{fn("View", "Update", 2, 369)}, map[string]int{"View.Update": none}},
		// The unchanged init (fingerprint 3) takes its own baseline, so the
		// edited one matches the other, at 9.
		{"duplicate name matches the unmatched one", []FunctionInfo{fn("", "init", 3, 4), fn("", "init", 5, 12)},
			map[string]int{"init": 9}},
		{"edited duplicates match in base order", []FunctionInfo{fn("", "init", 6, 12), fn("", "init", 7, 2)},
			map[string]int{"init#0": 4, "init#1": 9}},
		{"third duplicate has none", []FunctionInfo{fn("", "init", 3, 4), fn("", "init", 4, 9), fn("", "init", 5, 1)},
			map[string]int{"init": none}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := map[string]int{}
			seen := map[string]int{}
			for _, f := range ChangedFunctionsWithBase(base, tt.head) {
				k := f.QualifiedName()
				if _, dup := tt.want[k+"#0"]; dup {
					k += "#" + strconv.Itoa(seen[f.QualifiedName()])
					seen[f.QualifiedName()]++
				}
				got[k] = none
				if f.HasBase {
					got[k] = f.BaseCognitive
				}
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("ChangedFunctionsWithBase = %v, want %v", got, tt.want)
			}
			var names []string
			for _, f := range ChangedFunctions(base, tt.head) {
				names = append(names, f.QualifiedName())
			}
			if len(names) != len(tt.want) {
				t.Errorf("ChangedFunctions = %q, want the same %d functions", names, len(tt.want))
			}
		})
	}
}

func TestMostComplex(t *testing.T) {
	tests := []struct {
		name string
		cogs []int
		want int
	}{
		{"empty", nil, -1},
		{"one", []int{0}, 0},
		{"highest", []int{3, 40, 7}, 1},
		{"first on ties", []int{5, 9, 9}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fns := make([]FunctionInfo, len(tt.cogs))
			for i, c := range tt.cogs {
				fns[i].Cognitive = c
			}
			if got := MostComplex(fns); got != tt.want {
				t.Errorf("MostComplex = %d, want %d", got, tt.want)
			}
		})
	}
}
