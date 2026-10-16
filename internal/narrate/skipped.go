package narrate

import (
	"cmp"
	"slices"
	"strings"
)

// Skipped is a package the report leaves out because it was not analysed:
// it does not load, or none of its files is built, such as a Go directory
// whose files build constraints exclude (SPEC.md 6.5).
type Skipped struct {
	// Path is the package directory relative to the module root, as a
	// Package's Path.
	Path string `json:"path"`
	// Reason says why it was not analysed.
	Reason string `json:"reason"`
	// Error is the first load error of a package that does not load;
	// omitted for one none of whose files is built.
	Error string `json:"error,omitempty"`
}

// SkipGroup is the packages not analysed for one reason.
type SkipGroup struct {
	// Reason is the reason they share.
	Reason string
	// Paths are their paths, sorted.
	Paths []string
}

// GroupSkipped groups list by reason: the group with the most packages
// first, then by reason, each group's paths sorted. Every renderer lists
// the packages not analysed in this order.
func GroupSkipped(list []Skipped) []SkipGroup {
	var groups []SkipGroup
	for _, s := range list {
		i := slices.IndexFunc(groups, func(g SkipGroup) bool { return g.Reason == s.Reason })
		if i < 0 {
			groups = append(groups, SkipGroup{Reason: s.Reason})
			i = len(groups) - 1
		}
		groups[i].Paths = append(groups[i].Paths, s.Path)
	}
	for i := range groups {
		slices.Sort(groups[i].Paths)
	}
	slices.SortFunc(groups, func(a, b SkipGroup) int {
		return cmp.Or(cmp.Compare(len(b.Paths), len(a.Paths)), strings.Compare(a.Reason, b.Reason))
	})
	return groups
}
