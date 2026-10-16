package narrate

import "testing"

func TestNum(t *testing.T) {
	t.Parallel()

	for v, want := range map[float64]string{0: "0", 42.6: "42.6", 1000: "1,000", 16000: "16,000", 1234567.25: "1,234,567.3",
		-1500: "-1,500", 0.04: "0", 9.96: "10"} {
		if got := Num(v); got != want {
			t.Errorf("Num(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestNumRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		low, high float64
		want      string
	}{
		{328.2, 450.5, "328.2 to 450.5"},
		{1200, 1500, "1,200 to 1,500"},
		{2.46, 2.5, "2.5"},
		{0, 0, "0"},
	}
	for _, tt := range tests {
		if got := NumRange(tt.low, tt.high); got != tt.want {
			t.Errorf("NumRange(%v, %v) = %q, want %q", tt.low, tt.high, got, tt.want)
		}
	}
}
