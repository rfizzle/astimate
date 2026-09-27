package dupes

import "testing"

func TestDescribe(t *testing.T) {
	if got := Describe(-1); got != "negative" {
		t.Fatalf("Describe(-1) = %q, want %q", got, "negative")
	}
}
