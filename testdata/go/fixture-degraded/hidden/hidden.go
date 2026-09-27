// Package hidden carries package-level state, two init functions and one
// function nested four levels deep.
package hidden

import "example.com/fixture/hub"

var limit = 3

var (
	events  = make(chan int, 1)
	done    = make(chan struct{})
	counter int
)

func init() {
	limit = hub.Clamp(limit, 1, 10)
}

// Drain consumes pending events for every "wait" mode while active.
func Drain(active bool, modes []string) int {
	if active {
		for _, mode := range modes {
			switch mode {
			case "wait":
				select {
				case n := <-events:
					counter += n
				case <-done:
					return counter
				}
			}
		}
	}
	return counter
}
