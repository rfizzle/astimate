package score

import (
	"strings"
	"time"
)

// Tier classifies a package by agent_passes, per SPEC.md 7.4.
type Tier string

// Tier values, from rebuildable in one pass to not rebuildable as a unit.
const (
	TierOnePass   Tier = "ONE_PASS"
	TierFewPasses Tier = "FEW_PASSES"
	TierPartition Tier = "PARTITION"
)

// RebuildPrefix marks a config_version produced by the rebuild
// calibration in SPEC.md 11.2.
const RebuildPrefix = "rebuild-"

// thresholdsPrefix marks a config_version produced by a gate calibration,
// thresholds-<date> (SPEC.md 11.1 and 11.4).
const thresholdsPrefix = "thresholds-"

// rebuildFitDate is the date of the rebuild fit the shipped parameters
// come from (rebuild-2026-09-28-claude-code-opus). A gate calibration
// dated after it was made on top of the default that carries them.
const rebuildFitDate = "2026-09-28"

// TierOf returns the tier for agentPasses under t. Bounds are inclusive at the
// top: agentPasses <= t.OnePassMax is ONE_PASS, <= t.FewPassesMax is
// FEW_PASSES, anything larger is PARTITION.
func TierOf(agentPasses float64, t Tiers) Tier {
	switch {
	case agentPasses <= t.OnePassMax:
		return TierOnePass
	case agentPasses <= t.FewPassesMax:
		return TierFewPasses
	default:
		return TierPartition
	}
}

// Calibrated reports whether a config with this config_version carries rebuild
// parameters measured by rebuild experiments (SPEC.md 11.2): when the
// version starts with "rebuild-", or is a gate calibration thresholds-<date>
// dated after the rebuild fit, which retuned the gate of a default that
// already carried the fitted parameters and left them as they were. A gate
// calibration dated on or before the fit carries the placeholders. Any
// suffix after the date, such as a language override's "+typescript", is
// ignored. It takes the version string rather than the config so that
// score does not import config.
func Calibrated(configVersion string) bool {
	if strings.HasPrefix(configVersion, RebuildPrefix) {
		return true
	}
	rest, ok := strings.CutPrefix(configVersion, thresholdsPrefix)
	if !ok || len(rest) < len(time.DateOnly) {
		return false
	}
	date := rest[:len(time.DateOnly)]
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return false
	}
	return date > rebuildFitDate
}
