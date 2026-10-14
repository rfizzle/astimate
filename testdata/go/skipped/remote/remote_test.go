package remote

import (
	"testing"

	"example.com/skipped/base"
)

func TestCounter(t *testing.T) {
	c := base.New(1)
	c.Add(2)
	if digest("a") == 0 {
		t.Fatal("digest is zero")
	}
}
