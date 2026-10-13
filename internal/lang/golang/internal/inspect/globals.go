package inspect

import (
	"github.com/rfizzle/astimate/internal/lang/golang/internal/inspect/globals"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// GlobalCounts is the hidden-contract state of one package: package-level
// variables that hold mutable state and init functions (see globals.Counts).
type GlobalCounts = globals.Counts

// Globals counts the package-level variables that hold mutable state, and
// the init functions, declared in p's authored non-test files, leaving out
// the specs SPEC.md 6.5 names when written (ComplexityCounts.Written) holds
// none of their names. See globals.Count for the exclusions.
func Globals(m *load.Module, p *packages.Package, written map[string]bool) GlobalCounts {
	return globals.Count(m, p, written)
}
