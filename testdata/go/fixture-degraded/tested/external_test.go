package tested_test

import (
	"testing"

	"example.com/fixture/tested"
)

func TestCountUpper(t *testing.T) {
	if got := tested.CountUpper("AbC1"); got != 2 {
		t.Fatalf("CountUpper = %d, want 2", got)
	}
}
