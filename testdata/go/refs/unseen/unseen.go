// Package unseen implements refs.Shape but no test imports it, directly or
// through another package.
package unseen

// Tri implements refs.Shape.
type Tri struct{}

// Area has the method a test calls through refs.Shape, but no test binary
// holds a Tri.
func (Tri) Area() int { return 3 }
