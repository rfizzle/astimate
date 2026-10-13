// Package remote does not load: it imports a module that go.mod does not
// require and the module cache does not hold.
package remote

import (
	"example.com/absent/client"
	"example.com/skipped/base"
)

// Call calls the absent client with base.Value.
func Call() int { return client.Call(base.Value()) }
