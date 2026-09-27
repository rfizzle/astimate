package tested

import "unicode"

// CountUpper returns the number of upper-case letters in s.
func CountUpper(s string) int {
	count := 0
	for _, r := range s {
		if unicode.IsLetter(r) && unicode.IsUpper(r) {
			count++
		}
	}
	return count
}
