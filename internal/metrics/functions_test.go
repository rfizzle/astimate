package metrics

import (
	"slices"
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
