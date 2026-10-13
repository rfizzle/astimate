// Package consumer uses shared from non-test code and both shared and
// support from its test.
package consumer

import "example.com/rules/shared"

// Value returns shared's helper.
func Value() int { return shared.Helper() }
