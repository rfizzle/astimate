// Package tested has every exported function referenced from a test.
package tested

import (
	"strconv"
	"strings"

	"example.com/fixture/hub"
)

// Join renders nums as a comma-separated list.
func Join(nums []int) string {
	parts := make([]string, 0, len(nums))
	for _, n := range nums {
		parts = append(parts, strconv.Itoa(n))
	}
	return strings.Join(parts, ",")
}

// Bounded reports whether clamping n to [lo, hi] leaves it unchanged.
func Bounded(n, lo, hi int) bool {
	return hub.Clamp(n, lo, hi) == n
}
