package score

import (
	"cmp"
	"slices"
)

// maxDrivers is the number of terms SPEC.md 7.4 names as drivers.
const maxDrivers = 2

// Driver is one of the largest terms of rebuild_tokens, reported so a reader
// can see what makes a package expensive to rebuild.
type Driver struct {
	// Term is the term name, one of the Term* constants.
	Term string
	// Tokens is the term's contribution to rebuild_tokens.
	Tokens float64
	// Detail is the term's metric values, copied from Term.Detail.
	Detail string
}

// Drivers returns the two largest terms of r by Tokens, largest first. Terms
// with zero tokens are skipped, so fewer than two drivers may be returned.
// Ties keep the fixed term order of r.Terms.
func Drivers(r Rebuild) []Driver {
	ds := make([]Driver, 0, len(r.Terms))
	for _, tm := range r.Terms {
		if tm.Tokens > 0 {
			ds = append(ds, Driver{Term: tm.Name, Tokens: tm.Tokens, Detail: tm.Detail})
		}
	}
	slices.SortStableFunc(ds, func(a, b Driver) int {
		return cmp.Compare(b.Tokens, a.Tokens)
	})
	return ds[:min(len(ds), maxDrivers)]
}
