// Package refs has one exported func or method per way a test can refer to
// it, for the untested_exports reference table.
package refs

// Direct is called directly from a test.
func Direct() int { return 1 }

// Counter is used through a method value.
type Counter struct{ n int }

// Inc is taken as a method value in a test, then called.
func (c *Counter) Inc() { c.n++ }

// Shape is the interface Square is called through.
type Shape interface{ Area() int }

// Square implements Shape.
type Square struct{ Side int }

// Area is called only through a Shape value in a test.
func (s Square) Area() int { return s.Side * s.Side }

// Inner is embedded in Outer.
type Inner struct{}

// Hello is promoted to Outer and called on an Outer value in a test.
func (Inner) Hello() string { return "hello" }

// Outer embeds Inner.
type Outer struct{ Inner }

// Wrapper is intentionally untested and excluded by the directive.
//
//astimate:untested thin wrapper over Never
func Wrapper() int { return Never() }

// Never is referenced only from non-test code, which does not count.
func Never() int { return 2 }
