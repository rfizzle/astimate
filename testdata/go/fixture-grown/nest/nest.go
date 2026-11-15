// Package nest imports two packages of its own: one under its internal
// directory and one public child.
package nest

import (
	"example.com/fixture/nest/internal/wire"
	"example.com/fixture/nest/open"
)

// Join returns both children's values.
func Join() string { return wire.Value() + open.Value() }
