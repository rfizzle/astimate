package score

import (
	"reflect"
	"testing"
)

// rebuildOf builds a Rebuild with the five terms in fixed order and the given
// tokens, summing them into RebuildTokens.
func rebuildOf(volume, spec, contract, unspecified, hidden float64) Rebuild {
	names := []string{TermVolume, TermSpec, TermContract, TermUnspecified, TermHidden}
	tokens := []float64{volume, spec, contract, unspecified, hidden}
	r := Rebuild{Terms: make([]Term, 0, len(names))}
	for i, n := range names {
		r.Terms = append(r.Terms, Term{Name: n, Tokens: tokens[i], Detail: n + "-detail"})
		r.RebuildTokens += tokens[i]
	}
	return r
}

func driverTerms(ds []Driver) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Term)
	}
	return out
}

func TestDrivers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		r    Rebuild
		want []string
	}{
		{name: "largest two", r: rebuildOf(8000, 6000, 1200, 12000, 1200), want: []string{TermUnspecified, TermVolume}},
		{name: "tie keeps term order", r: rebuildOf(100, 500, 500, 500, 0), want: []string{TermSpec, TermContract}},
		{name: "tie for second", r: rebuildOf(900, 0, 0, 100, 100), want: []string{TermVolume, TermUnspecified}},
		{name: "zeros skipped", r: rebuildOf(0, 0, 0, 0, 400), want: []string{TermHidden}},
		{name: "all zero", r: rebuildOf(0, 0, 0, 0, 0), want: []string{}},
		{name: "no terms", r: Rebuild{}, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := driverTerms(Drivers(tt.r)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Drivers() terms = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDriversCarryTokensAndDetail(t *testing.T) {
	t.Parallel()

	ds := Drivers(rebuildOf(8000, 6000, 1200, 12000, 1200))
	want := []Driver{
		{Term: TermUnspecified, Tokens: 12000, Detail: TermUnspecified + "-detail"},
		{Term: TermVolume, Tokens: 8000, Detail: TermVolume + "-detail"},
	}
	if !reflect.DeepEqual(ds, want) {
		t.Errorf("Drivers() = %+v, want %+v", ds, want)
	}
}

func TestDriversDoesNotReorderTerms(t *testing.T) {
	t.Parallel()

	r := rebuildOf(1, 2, 3, 4, 5)
	_ = Drivers(r)
	if r.Terms[0].Name != TermVolume || r.Terms[4].Name != TermHidden {
		t.Errorf("Drivers mutated r.Terms: %+v", r.Terms)
	}
}
