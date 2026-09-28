package definition

import (
	"strings"

	"github.com/rfizzle/astimate/internal/score"
)

// ModRelDir returns the directory of pkg relative to the root of module, in
// slash form, "." for the root package.
func ModRelDir(module, pkg string) string {
	if pkg == module {
		return "."
	}
	return strings.TrimPrefix(pkg, module+"/")
}

// OwnPattern is the go package pattern of the module-relative directory
// dir, as ModRelDir returns it: "." for the root package, else "./dir".
func OwnPattern(dir string) string {
	if dir == "." {
		return "."
	}
	return "./" + dir
}

// Stratum is one cell of the selection: a tier and whether the package has
// tests of its own.
type Stratum struct {
	Tier   score.Tier
	Tested bool
}

// String names the stratum for reports.
func (s Stratum) String() string {
	if s.Tested {
		return string(s.Tier) + " tested"
	}
	return string(s.Tier) + " untested"
}

// Strata returns the six strata in selection order.
func Strata() []Stratum {
	out := make([]Stratum, 0, 6)
	for _, t := range tiers() {
		out = append(out, Stratum{t, true}, Stratum{t, false})
	}
	return out
}
