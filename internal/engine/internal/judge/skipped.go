package judge

import (
	"errors"
	"fmt"
	"slices"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

// ErrSkipped marks the failure of a package a check selected that its
// extractor skipped because it does not load (metrics.Skipper): the check
// cannot judge it, so its verdict is incomplete.
var ErrSkipped = errors.New("skipped, it does not load")

// Skips are the packages of one tree that its extractor skipped because
// they do not load. It is the one place a check consults a package's load
// status; a per-package status beside it would be read here too.
type Skips struct {
	// list holds the skipped packages sorted by identifier, their errors
	// relative to the module root.
	list []metrics.SkippedPackage
	// byPkg indexes list by identifier.
	byPkg map[string]*metrics.SkippedPackage
}

// SkippedIn returns the packages ext skipped in the module at root, none
// when ext does not implement metrics.Skipper. Each error names files
// relative to root (baseline.TreeRelative), so a staged copy's temporary
// root never shows.
func SkippedIn(ext metrics.Extractor, root string) (Skips, error) {
	sk, ok := ext.(metrics.Skipper)
	if !ok {
		return Skips{}, nil
	}
	list, err := sk.Skipped(root)
	if err != nil {
		return Skips{}, err
	}
	s := Skips{list: list, byPkg: make(map[string]*metrics.SkippedPackage, len(list))}
	for i := range s.list {
		p := &s.list[i]
		p.Error = baseline.TreeRelative(errors.New(p.Error), root).Error()
		s.byPkg[p.Package] = p
	}
	return s, nil
}

// With returns head, the identifiers Packages listed, with the skipped
// packages merged in and sorted, so changed-package selection sees a
// skipped package that changed; head itself when nothing was skipped.
func (s Skips) With(head []string) []string {
	if len(s.list) == 0 {
		return head
	}
	all := slices.Clone(head)
	for i := range s.list {
		all = append(all, s.list[i].Package)
	}
	slices.Sort(all)
	return slices.Compact(all)
}

// Failure returns the error a check records for pkg when it is skipped,
// wrapping ErrSkipped with the reason and first error, and nil otherwise.
func (s Skips) Failure(pkg string) error {
	p, ok := s.byPkg[pkg]
	if !ok {
		return nil
	}
	return fmt.Errorf("%w (%s): %s", ErrSkipped, p.Reason, p.Error)
}

// Report returns the skipped packages for report.Check.Skipped, named by
// module-relative directory in module modPath, each marked Selected when
// selected holds it.
func (s Skips) Report(modPath string, selected []string) []report.Skipped {
	if len(s.list) == 0 {
		return nil
	}
	out := make([]report.Skipped, 0, len(s.list))
	for i := range s.list {
		p := &s.list[i]
		out = append(out, report.Skipped{
			Package:  Rel(modPath, p.Package),
			Reason:   p.Reason,
			Error:    p.Error,
			Selected: slices.Contains(selected, p.Package),
		})
	}
	return out
}

// Unchanged returns selected, the identifiers of an every-package
// selection, without the skipped packages that changed holds no entry for:
// a skipped package is selected only when its own files changed since the
// baseline (SPEC.md 8.5), so a selection that covers every package leaves
// out the ones that cannot be judged and did not change. It returns
// selected itself when nothing is skipped.
func (s Skips) Unchanged(selected []string, changed map[string]bool) []string {
	if len(s.list) == 0 {
		return selected
	}
	out := make([]string, 0, len(selected))
	for _, pkg := range selected {
		if _, skipped := s.byPkg[pkg]; skipped && !changed[pkg] {
			continue
		}
		out = append(out, pkg)
	}
	return out
}

// Any reports whether the tree has a skipped package.
func (s Skips) Any() bool { return len(s.list) > 0 }
