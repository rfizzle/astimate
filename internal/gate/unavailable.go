package gate

// WithBaselineUnavailable tells Evaluate that the row's package existed at
// the baseline but could not be measured there, so base is nil because its
// history is unknown, not because the package is new (SPEC.md 8.1). The
// package is judged against the absolute limits only: the capacity ceilings
// and the max of every density rule apply as they do to a new package, but
// no rule ratchets from zero (ratchet_from_zero), since that would fail the
// blocks, exports and globals the package already had, and a requirement
// rule is not judged, since whether the package grew is unknown.
func WithBaselineUnavailable() Option {
	return func(o *options) { o.baselineUnavailable = true }
}
