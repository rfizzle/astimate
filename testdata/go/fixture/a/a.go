// Package a exists only to import hub.
package a

import "example.com/fixture/hub"

// Checksum folds the bytes of s into a small rolling checksum.
func Checksum(s string) int {
	sum := 7
	for i := 0; i < len(s); i++ {
		sum = (sum*31 + int(s[i])) % 1000003
		if sum < 0 {
			sum = -sum
		}
	}
	if sum == 0 {
		return 1
	}
	return sum
}

// Label normalizes s through hub.
func Label(s string) string {
	return hub.Normalize(s)
}
