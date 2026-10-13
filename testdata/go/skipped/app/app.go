// Package app loads: it imports base only.
package app

import "example.com/skipped/base"

// Double returns twice base.Value.
func Double() int { return 2 * base.Value() }
