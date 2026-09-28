package score

import (
	"strconv"
	"testing"
)

func TestTierOfBoundaries(t *testing.T) {
	t.Parallel()

	bounds := Tiers{OnePassMax: 1, FewPassesMax: 3}
	tests := []struct {
		passes float64
		want   Tier
	}{
		{0, TierOnePass},
		{1.0, TierOnePass},
		{1.1, TierFewPasses},
		{3.0, TierFewPasses},
		{3.1, TierPartition},
	}
	for _, tt := range tests {
		t.Run(strconv.FormatFloat(tt.passes, 'f', -1, 64), func(t *testing.T) {
			t.Parallel()
			if got := TierOf(tt.passes, bounds); got != tt.want {
				t.Errorf("TierOf(%v) = %s, want %s", tt.passes, got, tt.want)
			}
		})
	}
}

func TestTierOfCustomBounds(t *testing.T) {
	t.Parallel()

	bounds := Tiers{OnePassMax: 0.5, FewPassesMax: 2}
	if got := TierOf(0.8, bounds); got != TierFewPasses {
		t.Errorf("TierOf(0.8) = %s, want %s", got, TierFewPasses)
	}
	if got := TierOf(2.5, bounds); got != TierPartition {
		t.Errorf("TierOf(2.5) = %s, want %s", got, TierPartition)
	}
}

func TestCalibrated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version string
		want    bool
	}{
		{"default-uncalibrated-1", false},
		// Calibrated thresholds leave the rebuild parameters unmeasured.
		{"thresholds-2026-09-28", false},
		{"rebuild-2026-10-01-claude-code", true},
		{"", false},
		{"custom-rebuild-1", false},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			t.Parallel()
			if got := Calibrated(tt.version); got != tt.want {
				t.Errorf("Calibrated(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}
