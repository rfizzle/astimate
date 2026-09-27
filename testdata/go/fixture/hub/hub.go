// Package hub is imported by four other packages in the fixture and itself
// imports one standard-library package and one external module.
package hub

import (
	"strings"

	"example.com/extmod"
)

// Normalize trims and lower-cases s, then tags it.
func Normalize(s string) string {
	return extmod.Tag(strings.ToLower(strings.TrimSpace(s)))
}

// Clamp limits n to the closed range [lo, hi].
func Clamp(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// Twice returns n doubled.
func Twice(n int) int {
	return double(n)
}
