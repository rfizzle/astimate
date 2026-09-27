// Package broken does not type-check. It exists to test load errors.
package broken

// Count returns a string where an int is declared.
func Count() int {
	return "one"
}
