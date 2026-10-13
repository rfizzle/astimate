// Package support is imported only by consumer's test file, so it is a
// test-support package: its own non-test code runs only under tests.
package support

// Fake is a test double.
type Fake struct{}

// Do is called from Run, a non-test helper of this package.
func (Fake) Do() int { return 1 }

// Run is called by consumer's test.
func Run() int { return Fake{}.Do() }
