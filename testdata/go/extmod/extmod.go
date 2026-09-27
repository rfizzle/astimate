// Package extmod stands in for a third-party dependency of the fixture
// module. The fixture reaches it through a local replace directive.
package extmod

// Tag prefixes s with a fixed marker.
func Tag(s string) string {
	return "ext:" + s
}
