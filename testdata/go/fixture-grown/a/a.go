// Package a exists only to import hub.
package a

import "example.com/fixture/hub"

// Label normalizes s through hub.
func Label(s string) string {
	return hub.Normalize(s)
}
