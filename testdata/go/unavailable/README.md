# unavailable

A module for the case where a package existed at the baseline but could not
be analysed there. `blocks` holds one duplicate block (`First` and `Second`)
in otherwise distinct code. The engine test commits it beside a `broken.go`
that does not type-check, so the baseline skips the package (a type error),
and then without it, so head loads it with the same block: the package is
judged against the maxes only, with the ratchets skipped. `fresh` is the
same code in a package the baseline did not have at all; the test adds it in
the head commit, and it is new, so `dup_blocks` ratchets from zero and fails.
