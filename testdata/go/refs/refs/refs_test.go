package refs

import "testing"

func TestDirect(t *testing.T) {
	if got := Direct(); got != 1 {
		t.Fatalf("Direct() = %d, want 1", got)
	}
}

func TestMethodValue(t *testing.T) {
	var c Counter
	inc := c.Inc
	inc()
	if c.n != 1 {
		t.Fatalf("n = %d, want 1", c.n)
	}
}

func TestPromoted(t *testing.T) {
	var o Outer
	if got := o.Hello(); got != "hello" {
		t.Fatalf("Hello() = %q, want %q", got, "hello")
	}
}
