// Package support is a test-support package: only test files import it.
package support

import "testing"

// Equal fails tb when got is not want.
func Equal(tb testing.TB, got, want string) {
	tb.Helper()
	if got != want {
		tb.Errorf("got %q, want %q", got, want)
	}
}
