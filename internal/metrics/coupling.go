package metrics

import "math"

// Instability returns Robert Martin's instability Ce / (Ca + Ce) with
// Ca = fanIn and Ce = internalImports, both counting module-internal edges
// only. It returns nil when Ca + Ce is not positive, since a package with no
// internal edges has no defined instability. The result is in [0, 1] for
// non-negative inputs.
func Instability(fanIn, internalImports int) *float64 {
	total := fanIn + internalImports
	if total <= 0 {
		return nil
	}
	v := float64(internalImports) / float64(total)
	return &v
}

// Abstractness returns exported interface types over all exported types. It
// returns nil when exportedTypes is not positive, since a package with no
// exported types has no defined abstractness.
func Abstractness(exportedInterfaceTypes, exportedTypes int) *float64 {
	if exportedTypes <= 0 {
		return nil
	}
	v := float64(exportedInterfaceTypes) / float64(exportedTypes)
	return &v
}

// MainSequenceDistance returns |abstractness + instability - 1|, the distance
// from Martin's main sequence A + I = 1. It returns nil when either input is
// nil.
func MainSequenceDistance(abstractness, instability *float64) *float64 {
	if abstractness == nil || instability == nil {
		return nil
	}
	v := math.Abs(*abstractness + *instability - 1)
	return &v
}

// ratioScale is 10 to the number of decimal places RoundRatio keeps.
const ratioScale = 1e3

// RoundRatio returns v rounded half away from zero to three decimal places,
// or nil for a nil v, so a ratio reports 0.2, not 0.19999999999999996, and
// 0, not 1.1e-16. Extractors round instability, abstractness and
// main_sequence_distance with it, computing the distance from the unrounded
// ratios.
func RoundRatio(v *float64) *float64 {
	if v == nil {
		return nil
	}
	r := math.Round(*v*ratioScale) / ratioScale
	return &r
}
