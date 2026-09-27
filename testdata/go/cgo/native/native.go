// Package native wraps a trivial C function through cgo.
package native

/*
static int add(int a, int b) { return a + b; }
*/
import "C"

// Add returns a plus b, computed in C.
func Add(a, b int) int {
	return int(C.add(C.int(a), C.int(b)))
}
