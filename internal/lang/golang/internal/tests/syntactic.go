package tests

import (
	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/tests/syntactic"
)

// indexSkipped adds to the direct index the funcs and methods that the test
// files of the packages skipped because they fail to load refer to, read
// by name (package syntactic; SPEC.md 6.4). They count as references from
// another package's tests, like any a skipped package makes to its
// neighbours.
func (r *Refs) indexSkipped(m *load.Module) {
	for _, fn := range syntactic.Funcs(m) {
		r.addDirect(m, "", fn, false)
	}
}
