package gate

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/rfizzle/astimate/internal/metrics"
)

// Exemption silences the violations of one rule on one row, with the reason
// recorded beside it (SPEC.md 8.6). A silenced violation does not fail the
// gate but is still reported, as exempted, with that reason.
type Exemption struct {
	// Package is the row the exemption applies to: a package directory
	// relative to the module root in slash form, "." for the root package,
	// or metrics.ModuleRowID for the module row.
	Package string
	// Metric is the metric whose rule the exemption silences.
	Metric string
	// Reason says why the violation is accepted; it is required.
	Reason string
	// Expires is the last day the exemption applies, as YYYY-MM-DD; empty
	// means it never expires.
	Expires string
}

// Validate checks the exemption's fields: a package of the form Package
// documents, a metric that gated reports some threshold gates, a non-empty
// reason and an optional date. A module-wide metric (metrics.ModuleWide) is
// gated on the module row only and every other metric on package rows only,
// so the package must agree with the metric. All errors are joined with
// errors.Join.
func (e Exemption) Validate(gated func(metric string) bool) error {
	var errs []error
	pkgOK := validPackage(e.Package)
	switch {
	case e.Package == "":
		errs = append(errs, errors.New("package is required"))
	case !pkgOK:
		errs = append(errs, fmt.Errorf("package %q: want a module-relative directory such as internal/x, . for the root package, or %s",
			e.Package, metrics.ModuleRowID))
	}
	switch {
	case e.Metric == "":
		errs = append(errs, errors.New("metric is required"))
	case !gated(e.Metric):
		errs = append(errs, fmt.Errorf("metric %q: no threshold gates it", e.Metric))
	case pkgOK && metrics.ModuleWide(e.Metric) && e.Package != metrics.ModuleRowID:
		errs = append(errs, fmt.Errorf("metric %q is gated on the module row only; its package must be %s",
			e.Metric, metrics.ModuleRowID))
	case pkgOK && !metrics.ModuleWide(e.Metric) && e.Package == metrics.ModuleRowID:
		errs = append(errs, fmt.Errorf("metric %q is gated on package rows only; %s carries module-wide metrics",
			e.Metric, metrics.ModuleRowID))
	}
	if strings.TrimSpace(e.Reason) == "" {
		errs = append(errs, errors.New("reason is required"))
	}
	if e.Expires != "" {
		if _, err := parseDate(e.Expires); err != nil {
			errs = append(errs, fmt.Errorf("expires %q: want a date such as 2026-12-31", e.Expires))
		}
	}
	return errors.Join(errs...)
}

// Expired reports whether the exemption no longer applies at now: its
// Expires date is before now's calendar date, in now's location. The
// exemption applies through the whole of that day. An Expires that is not
// a date, which Validate rejects, counts as expired, so an exemption that
// cannot be read never silences anything.
func (e Exemption) Expired(now time.Time) bool {
	if e.Expires == "" {
		return false
	}
	d, err := parseDate(e.Expires)
	if err != nil {
		return true
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return today.After(d)
}

// Matches reports whether the exemption applies to v, a violation on the
// row whose package path is pkg: the same row and the same metric. It does
// not consider expiry.
func (e Exemption) Matches(pkg string, v *Violation) bool {
	return e.Package == pkg && e.Metric == v.Metric
}

// validPackage reports whether p is metrics.ModuleRowID, "." or a clean,
// relative, slash-separated directory inside the module.
func validPackage(p string) bool {
	switch {
	case p == metrics.ModuleRowID || p == ".":
		return true
	case p == "" || strings.ContainsAny(p, `\<>`) || path.IsAbs(p) || path.Clean(p) != p:
		return false
	}
	return p != ".." && !strings.HasPrefix(p, "../")
}

// parseDate parses s as a YYYY-MM-DD date, accepting only that canonical
// form.
func parseDate(s string) (time.Time, error) {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, err
	}
	if d.Format(time.DateOnly) != s {
		return time.Time{}, fmt.Errorf("date %q is not in YYYY-MM-DD form", s)
	}
	return d, nil
}

// Exempted is a violation, or a warn rule's breach, that an exemption
// silenced, with the exemption's reason.
type Exempted struct {
	Violation
	// Reason is the matching exemption's reason.
	Reason string
}

// Exemptions applies a list of exemptions to the rows of one check and
// remembers which of them matched, so that the check can report the ones
// that matched nothing. It is not safe for concurrent use. Create one with
// NewExemptions.
type Exemptions struct {
	list    []Exemption
	expired []bool
	used    []bool
}

// NewExemptions returns the exemptions of list as they stand at now: an
// exemption Expired at now never applies.
func NewExemptions(list []Exemption, now time.Time) *Exemptions {
	x := &Exemptions{list: list, expired: make([]bool, len(list)), used: make([]bool, len(list))}
	for i, e := range list {
		x.expired[i] = e.Expired(now)
	}
	return x
}

// Apply moves each violation of res, the result of the row whose package
// path is pkg, that an unexpired exemption matches from res.Violations to
// res.Exempted, then each breach of a warn rule (a warning whose Severity
// is SeverityWarn) that one matches from res.Warnings, carrying the reason
// of the first matching exemption in list order, marks every matching
// exemption as used, and sets res.Passed from the violations that remain.
// Other warnings are never exempted: they never fail the gate, so an
// exemption on a capacity rule matches only its violation. An exemption
// with a blank reason, which Validate rejects, never matches, so no
// finding is silenced without a reason to show. A nil x exempts nothing.
func (x *Exemptions) Apply(pkg string, res *Result) {
	if x != nil && len(x.list) > 0 {
		res.Violations = x.exempt(pkg, res, res.Violations, func(*Violation) bool { return true })
		res.Warnings = x.exempt(pkg, res, res.Warnings, func(w *Violation) bool { return w.Severity == SeverityWarn })
	}
	res.Passed = len(res.Violations) == 0
}

// exempt moves each finding of fs for which eligible is true and that an
// unexpired exemption matches on row pkg to res.Exempted, and returns the
// findings left, in their order.
func (x *Exemptions) exempt(pkg string, res *Result, fs []Violation, eligible func(*Violation) bool) []Violation {
	kept := make([]Violation, 0, len(fs))
	for i := range fs {
		v := &fs[i]
		if !eligible(v) {
			kept = append(kept, *v)
			continue
		}
		reason, ok := x.match(pkg, v)
		if !ok {
			kept = append(kept, *v)
			continue
		}
		res.Exempted = append(res.Exempted, Exempted{Violation: *v, Reason: reason})
	}
	return kept
}

// match returns the reason of the first unexpired exemption matching v on
// row pkg, marking every such exemption used, and whether there was one.
func (x *Exemptions) match(pkg string, v *Violation) (string, bool) {
	reason, ok := "", false
	for i := range x.list {
		// Validate requires a reason; one built without it never silences
		// a finding, whose reason every output must show.
		if x.expired[i] || strings.TrimSpace(x.list[i].Reason) == "" || !x.list[i].Matches(pkg, v) {
			continue
		}
		x.used[i] = true
		if !ok {
			reason, ok = x.list[i].Reason, true
		}
	}
	return reason, ok
}

// Expired returns the exemptions that were expired when x was created, in
// list order.
func (x *Exemptions) Expired() []Exemption {
	if x == nil {
		return nil
	}
	var out []Exemption
	for i, e := range x.list {
		if x.expired[i] {
			out = append(out, e)
		}
	}
	return out
}

// Stale returns the unexpired exemptions that matched no violation in any
// call to Apply, in list order, leaving out each one for which unknown
// reports true: an exemption whose row the check could not judge, such as
// a package that failed to extract, is not known to be stale. Only a check
// that judged every row can call an exemption stale; after a check of some
// packages, an exemption on another package matched nothing because its
// row was never evaluated, so such a check must not call Stale.
func (x *Exemptions) Stale(unknown func(Exemption) bool) []Exemption {
	if x == nil {
		return nil
	}
	var out []Exemption
	for i, e := range x.list {
		if !x.expired[i] && !x.used[i] && !unknown(e) {
			out = append(out, e)
		}
	}
	return out
}
