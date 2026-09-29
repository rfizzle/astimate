// Package sibling has a constructor that only another package's test
// calls.
package sibling

// New returns a greeting for name.
func New(name string) string { return "hello " + name }

// Orphan is called from no test anywhere.
func Orphan() int { return 0 }
