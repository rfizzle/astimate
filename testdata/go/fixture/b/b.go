// Package b exists only to import hub.
package b

import "example.com/fixture/hub"

// Limit clamps n to the range [0, 10] through hub.
func Limit(n int) int {
	return hub.Clamp(n, 0, 10)
}
