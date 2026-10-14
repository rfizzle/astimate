// Package remote does not load: it imports a module that go.mod does not
// require and the module cache does not hold.
package remote

import (
	"example.com/absent/client"
	"example.com/skipped/base"
)

// Call calls the absent client with base.Value.
func Call() int { return client.Call(base.Value()) }

// digest is a copy of base.Checksum.
func digest(s string) int {
	sum := 7
	for i := 0; i < len(s); i++ {
		sum = (sum*31 + int(s[i])) % 1000003
		if sum < 0 {
			sum = -sum
		}
	}
	return sum
}
