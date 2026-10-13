// Package user's test exercises lib.
package user

import "example.com/refs/refs"

// The non-test code of this package imports refs, so refs is not a
// test-support package: only test files of user use it otherwise.
var _ refs.Shape = refs.Square{}
