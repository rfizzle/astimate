package tested

import (
	"slices"
	"testing"
)

func TestRoman(t *testing.T) {
	cases := map[int]string{0: "", 4: "IV", 1994: "MCMXCIV", 4000: ""}
	for n, want := range cases {
		if got := Roman(n); got != want {
			t.Errorf("Roman(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestRunLength(t *testing.T) {
	if got := RunLength("aaabccdddddddddddd"); got != "a3bc2d12" {
		t.Fatalf("RunLength = %q, want %q", got, "a3bc2d12")
	}
}

func TestBalanced(t *testing.T) {
	if !Balanced("f(a[0], {b})") {
		t.Error("Balanced(f(a[0], {b})) = false, want true")
	}
	if Balanced("(]") || Balanced("((") {
		t.Error("Balanced reported an unbalanced string as balanced")
	}
}

func TestMedian(t *testing.T) {
	if _, ok := Median(nil); ok {
		t.Error("Median(nil) ok = true, want false")
	}
	if got, _ := Median([]float64{3, 1, 2}); got != 2 {
		t.Errorf("Median odd = %v, want 2", got)
	}
	if got, _ := Median([]float64{4, 1, 3, 2}); got != 2.5 {
		t.Errorf("Median even = %v, want 2.5", got)
	}
}

func TestTopWords(t *testing.T) {
	got := TopWords("the cat and the hat; The end.", 2)
	if want := []string{"the", "and"}; !slices.Equal(got, want) {
		t.Fatalf("TopWords = %q, want %q", got, want)
	}
	if TopWords("x", 0) != nil {
		t.Error("TopWords with k 0 is not nil")
	}
}
