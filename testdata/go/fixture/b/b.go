// Package b exists only to import hub.
package b

import "example.com/fixture/hub"

// Limit clamps n to the range [0, 10] through hub.
func Limit(n int) int {
	return hub.Clamp(n, 0, 10)
}

// Digest is a copy of a.Checksum under other names, the cross-package
// duplicate the fixture measures.
func Digest(text string) int {
	acc := 11
	for j := 0; j < len(text); j++ {
		acc = (acc*37 + int(text[j])) % 999983
		if acc < 0 {
			acc = -acc
		}
	}
	if acc == 0 {
		return 1
	}
	return acc
}
