package refs_test

import (
	"testing"

	"example.com/refs/refs"
)

func TestInterfaceDispatch(t *testing.T) {
	var s refs.Shape = refs.Square{Side: 3}
	if got := s.Area(); got != 9 {
		t.Fatalf("Area() = %d, want 9", got)
	}
}

func TestGenericInterfaceDispatch(t *testing.T) {
	var g refs.Getter[string] = refs.Box[string]{V: "v"}
	if got := g.Get(); got != "v" {
		t.Fatalf("Get() = %q, want %q", got, "v")
	}
}
