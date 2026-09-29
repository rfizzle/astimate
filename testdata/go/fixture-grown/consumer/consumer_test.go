package consumer

import (
	"testing"

	"example.com/fixture/sibling"
	"example.com/fixture/support"
)

func TestGreeting(t *testing.T) {
	support.Equal(t, sibling.New(Name()), "hello x")
}
