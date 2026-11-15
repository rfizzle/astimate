package metrics

import (
	"errors"
	"fmt"
	"math"
)

// ErrInvalidMetrics is wrapped by every error returned from
// RawMetrics.Validate.
var ErrInvalidMetrics = errors.New("invalid metrics")

// Validate reports every field that is out of range: negative counts,
// percentages outside [0, 100], ratios outside [0, 1], largest_file_sloc
// above sloc, tokens_est_with_tests below tokens_est, a positive
// tokens_est_generated with generated_files 0, documented_exports above
// exported_symbols, and internal_imports_private above
// internal_imports_descendants or either above internal_imports. The
// returned error
// joins one error per problem, each wrapping ErrInvalidMetrics; it is nil when
// the record is consistent. Nil v1 fields are not checked.
func (m *RawMetrics) Validate() error {
	var errs []error
	t, slots := fieldTable(), m.slots()
	for i := range t {
		v, ok, k := read(slots[i])
		switch {
		case !ok || k == kindBool:
		case k == kindFloat && (math.IsNaN(v) || v < 0 || v > t[i].upper):
			errs = append(errs, fmt.Errorf("%w: %s %v outside [0, %v]", ErrInvalidMetrics, t[i].name, v, t[i].upper))
		case k == kindInt && v < 0:
			errs = append(errs, fmt.Errorf("%w: %s is negative (%d)", ErrInvalidMetrics, t[i].name, int(v)))
		}
	}

	if m.LargestFileSLOC > m.SLOC {
		errs = append(errs, fmt.Errorf("%w: largest_file_sloc %d exceeds sloc %d",
			ErrInvalidMetrics, m.LargestFileSLOC, m.SLOC))
	}
	if m.TokensEstWithTests < m.TokensEst {
		errs = append(errs, fmt.Errorf("%w: tokens_est_with_tests %d is below tokens_est %d",
			ErrInvalidMetrics, m.TokensEstWithTests, m.TokensEst))
	}
	if m.TokensEstGenerated != nil && *m.TokensEstGenerated > 0 && m.GeneratedFiles != nil && *m.GeneratedFiles == 0 {
		errs = append(errs, fmt.Errorf("%w: tokens_est_generated %d with generated_files 0",
			ErrInvalidMetrics, *m.TokensEstGenerated))
	}
	if m.DocumentedExports != nil && *m.DocumentedExports > m.ExportedSymbols {
		errs = append(errs, fmt.Errorf("%w: documented_exports %d exceeds exported_symbols %d",
			ErrInvalidMetrics, *m.DocumentedExports, m.ExportedSymbols))
	}
	if d := m.InternalImportsDescendants; d != nil && *d > m.InternalImports {
		errs = append(errs, fmt.Errorf("%w: internal_imports_descendants %d exceeds internal_imports %d",
			ErrInvalidMetrics, *d, m.InternalImports))
	}
	if p := m.InternalImportsPrivate; p != nil {
		bound, name := m.InternalImports, "internal_imports"
		if d := m.InternalImportsDescendants; d != nil {
			bound, name = *d, "internal_imports_descendants"
		}
		if *p > bound {
			errs = append(errs, fmt.Errorf("%w: internal_imports_private %d exceeds %s %d", ErrInvalidMetrics, *p, name, bound))
		}
	}
	return errors.Join(errs...)
}
