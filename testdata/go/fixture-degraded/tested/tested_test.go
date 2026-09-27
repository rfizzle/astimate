package tested

import "testing"

func TestJoin(t *testing.T) {
	if got := Join([]int{1, 2, 3}); got != "1,2,3" {
		t.Fatalf("Join = %q, want %q", got, "1,2,3")
	}
}

func TestBounded(t *testing.T) {
	if !Bounded(5, 0, 10) {
		t.Fatal("Bounded(5, 0, 10) = false, want true")
	}
	if Bounded(11, 0, 10) {
		t.Fatal("Bounded(11, 0, 10) = true, want false")
	}
}
