package score

import "strings"

// Tier classifies a package by agent_passes, per SPEC.md 7.4.
type Tier string

// Tier values, from rebuildable in one pass to not rebuildable as a unit.
const (
	TierOnePass   Tier = "ONE_PASS"
	TierFewPasses Tier = "FEW_PASSES"
	TierPartition Tier = "PARTITION"
)

// calibratedPrefix marks a config_version produced by the rebuild
// calibration in SPEC.md 11.2.
const calibratedPrefix = "rebuild-"

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
// parameters measured by rebuild experiments (SPEC.md 11.2), which is when the
// version starts with "rebuild-". It takes the version string rather than the
// config so that score does not import config.
func Calibrated(configVersion string) bool {
	return strings.HasPrefix(configVersion, calibratedPrefix)
}
