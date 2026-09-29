package user

import (
	"testing"

	"example.com/refs/lib"
	"example.com/refs/refs"
)

func TestLib(t *testing.T) {
	if got := lib.Lib(); got != 1 {
		t.Fatalf("Lib() = %d, want 1", got)
	}
	var s refs.Shape = lib.Impl{}
	if got := s.Area(); got != 4 {
		t.Fatalf("Area() = %d, want 4", got)
	}
}
