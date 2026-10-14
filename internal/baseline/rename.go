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
	// carried maps a package with a row of its own to the packages carved
	// out in this change whose import by it is not a rise in its
	// internal_imports (Seeded).
	carried map[string]map[string]bool
}

// Seeded returns b with rows seeded from renames, the renames since the
// baseline (FindRenames). A package without a row of its own in b whose
// files were renamed from packages that have one is served a row seeded
// from them: the metrics of the source most files came from (ties going
// to the first directory), the functions, duplicate blocks and untested
// exports of every source, and From naming every source. The seeded row
// records duplicate blocks and untested exports (Row.HasNamed) only when
// every source does, since a source without them would read all of its
// blocks as added. imports reports whether the package in the directory
// from imports the package in to at head; nil means none does. An import
// of a package carved out in this change is not a rise: the seeded row's
// fan_in counts each source importing it, and a package's own row counts
// in its internal_imports each carved-out package it imports when it is
// one of that package's sources, or when it imported a source in b
// (b.Importers) and still imports that source at head, so the import only
// follows code to where it now lives. b's own rows, the module row
// included, are otherwise served unchanged. With no renames Seeded
// returns b.
func Seeded(b Baseline, renames Renames, imports func(from, to string) bool) Baseline {
	if len(renames) == 0 {
		return b
	}
	if imports == nil {
		imports = func(string, string) bool { return false }
	}
	s := &seeded{Baseline: b, renames: renames, imports: imports, carried: make(map[string]map[string]bool)}
	for to, srcs := range renames {
		if _, ok := b.Row(to); ok {
			continue
		}
		isSource := make(map[string]bool, len(srcs))
		for _, src := range srcs {
			isSource[src.Dir] = true
		}
		for _, src := range srcs {
			if imports(src.Dir, to) {
				s.carry(src.Dir, to)
			}
			importers, _ := b.Importers(src.Dir)
			for _, p := range importers {
				if !isSource[p] && p != to && imports(p, to) && imports(p, src.Dir) {
					s.carry(p, to)
				}
			}
		}
	}
	return s
}

// carry records that the import of the carved-out package to by the
// package in dir is not a rise.
func (s *seeded) carry(dir, to string) {
	if s.carried[dir] == nil {
		s.carried[dir] = make(map[string]bool)
	}
	s.carried[dir][to] = true
}

// Row implements Baseline.
func (s *seeded) Row(dir string) (Row, bool) {
	if r, ok := s.Baseline.Row(dir); ok {
		r.Metrics.InternalImports += len(s.carried[dir])
		return r, true
	}
	var seed Row
	named := true
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
		named = named && r.HasNamed
		seed.Named.DupBlocks = append(seed.Named.DupBlocks, r.Named.DupBlocks...)
		seed.Named.UntestedExports = append(seed.Named.UntestedExports, r.Named.UntestedExports...)
		if s.imports(src.Dir, dir) {
			seed.Metrics.FanIn++
		}
		seed.From = append(seed.From, src.Dir)
	}
	if seed.From == nil {
		return Row{}, false
	}
	slices.Sort(seed.From)
	if !named {
		seed.Named = Named{}
		return seed, true
	}
	seed.HasNamed = true
	slices.Sort(seed.Named.DupBlocks)
	seed.Named.DupBlocks = slices.Compact(seed.Named.DupBlocks)
	slices.Sort(seed.Named.UntestedExports)
	return seed, true
}
