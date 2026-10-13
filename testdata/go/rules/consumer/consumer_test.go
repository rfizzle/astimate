package consumer

import (
	"testing"

	"example.com/rules/shared"
	"example.com/rules/support"
)

func TestValue(t *testing.T) {
	if Value() != shared.Helper() || support.Run() != 1 {
		t.Fatal("unexpected value")
	}
}
