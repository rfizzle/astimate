package score_test

import (
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/score"
)

// TestDefaultConfigTiersAndCalibration checks the tier bounds and calibration
// flag the embedded default config yields, end to end through config.Parse.
func TestDefaultConfigTiersAndCalibration(t *testing.T) {
	t.Parallel()

	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatalf("config.Parse(config.Default()) error = %v", err)
	}
	if !score.Calibrated(cfg.Version) {
		t.Errorf("Calibrated(%q) = false, want true: the default ships fitted rebuild parameters", cfg.Version)
	}
	tests := []struct {
		passes float64
		want   score.Tier
	}{
		{1.0, score.TierOnePass},
		{1.1, score.TierFewPasses},
		{3.0, score.TierFewPasses},
		{3.1, score.TierPartition},
	}
	for _, tt := range tests {
		if got := score.TierOf(tt.passes, cfg.Rebuild.Tiers); got != tt.want {
			t.Errorf("TierOf(%v, default tiers) = %s, want %s", tt.passes, got, tt.want)
		}
	}
}
