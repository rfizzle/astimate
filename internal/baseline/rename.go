package baseline

import "slices"

// seeded is a Baseline that serves a moved package the row of the packages
// its files were renamed from (Seeded).
type seeded struct {
	Baseline
	renames Renames
	// imports reports whether the package in from imports the package in
	// to at head.
	imports func(from, to string) bool
	// carved maps a baseline package to the packages without a row of
	// their own that files were renamed into from it.
	carved map[string][]string
}

// Seeded returns b with rows seeded from renames, the renames since the
// baseline (FindRenames). A package without a row of its own in b whose
// files were renamed from packages that have one is served a row seeded
// from them: the metrics of the source most files came from (ties going
// to the first directory), the functions of every source, and From naming
// every source. imports reports whether the package in the directory from
// imports the package in to at head; nil means none does. The edge from a
// source to a package carved out of it is not a change: the seeded row's
// fan_in counts each source importing it, and a source's own row counts
// each package carved out of it that it imports in its internal_imports.
// b's own rows, the module row included, are otherwise served unchanged.
// With no renames Seeded returns b.
func Seeded(b Baseline, renames Renames, imports func(from, to string) bool) Baseline {
	if len(renames) == 0 {
		return b
	}
	if imports == nil {
		imports = func(string, string) bool { return false }
	}
	s := &seeded{Baseline: b, renames: renames, imports: imports, carved: make(map[string][]string)}
	for to, srcs := range renames {
		if _, ok := b.Row(to); ok {
			continue
		}
		for _, src := range srcs {
			s.carved[src.Dir] = append(s.carved[src.Dir], to)
		}
	}
	return s
}

// Row implements Baseline.
func (s *seeded) Row(dir string) (Row, bool) {
	if r, ok := s.Baseline.Row(dir); ok {
		for _, to := range s.carved[dir] {
			if s.imports(dir, to) {
				r.Metrics.InternalImports++
			}
		}
		return r, true
	}
	var seed Row
	for _, src := range s.renames[dir] {
		r, ok := s.Baseline.Row(src.Dir)
		if !ok {
			continue
		}
		if seed.From == nil {
			seed = Row{Metrics: r.Metrics, HasFunctions: r.HasFunctions}
		}
		if r.HasFunctions {
			seed.Functions = append(seed.Functions, r.Functions...)
		}
		if s.imports(src.Dir, dir) {
			seed.Metrics.FanIn++
		}
		seed.From = append(seed.From, src.Dir)
	}
	if seed.From == nil {
		return Row{}, false
	}
	slices.Sort(seed.From)
	return seed, true
}
