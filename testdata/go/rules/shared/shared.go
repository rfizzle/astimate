// Package shared is imported by consumer's non-test code, so it is not a
// test-support package and its own calls prove nothing.
package shared

// Thing is a type of this package.
type Thing struct{}

// Go is called from Helper, a non-test function of this package.
func (Thing) Go() int { return 1 }

// Helper is called by consumer and by its test.
func Helper() int { return Thing{}.Go() }
