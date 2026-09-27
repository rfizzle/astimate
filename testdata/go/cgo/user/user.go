// Package user is pure Go and imports the cgo package native.
package user

import "example.com/cgo/native"

// Sum adds the numbers in xs through native.Add.
func Sum(xs ...int) int {
	total := 0
	for _, x := range xs {
		total = native.Add(total, x)
	}
	return total
}
