// Package lib has no tests of its own; another package's test calls it.
package lib

// Lib is called directly from user's test.
func Lib() int { return 1 }

// Unused is called from no test file anywhere.
func Unused() int { return 2 }

// Impl implements refs.Shape.
type Impl struct{}

// Area is called only through a refs.Shape value in user's test.
func (Impl) Area() int { return 4 }
