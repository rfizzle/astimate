package typescript

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/rfizzle/astimate/internal/lang/duptok"
	"github.com/rfizzle/astimate/internal/metrics"
)

// assembleOptions carries the extractor configuration assemble needs.
type assembleOptions struct {
	charsPerToken float64
	// o200k selects the o200k_base counts the load recorded instead of the
	// byte ratio.
	o200k bool
	dup   duptok.Options
}

// assemble computes the metrics of package p of module m from the facts
// its files recorded at load, and records its details in m.
func assemble(m *module, p *pkg, opts assembleOptions) (metrics.RawMetrics, error) {
	var r metrics.RawMetrics
	var scores []int
	var types, interfaces int
	var srcBytes, srcTokens, allBytes, allTokens int
	var candidates []exportedFunc
	inits := 0
	for _, f := range p.src {
		r.Files++
		r.SLOC += f.sloc
		r.LargestFileSLOC = max(r.LargestFileSLOC, f.sloc)
		srcBytes += f.size
		srcTokens += f.o200k
		r.ExportedSymbols += f.exports
		types += f.exportedTypes
		interfaces += f.exportedInterfaces
		r.Globals += f.globals
		if f.hasInit {
			inits++
		}
		for _, fn := range f.funcs {
			scores = append(scores, fn.cognitive)
			r.CognitiveTotal += fn.cognitive
			r.MaxNesting = max(r.MaxNesting, fn.nesting)
		}
		candidates = append(candidates, f.exportedFuncs...)
	}
	r.InitFuncs = inits
	r.ExportedSymbols += p.reexports
	r.FuncCount = len(scores)
	r.CognitiveP90 = p90(scores)

	referenced := map[string]bool{}
	allBytes, allTokens = srcBytes, srcTokens
	for _, f := range p.tests {
		r.TestFiles++
		r.TestFuncs += f.testFuncs
		allBytes += f.size
		allTokens += f.o200k
		for id := range f.idents {
			referenced[id] = true
		}
	}
	r.HasTests = r.TestFuncs > 0
	var untested []string
	for _, c := range candidates {
		if !referenced[c.match] {
			untested = append(untested, c.display)
		}
	}
	slices.Sort(untested)
	r.UntestedExports = len(untested)

	if opts.o200k {
		r.TokensEst, r.TokensEstWithTests = srcTokens, allTokens
	} else {
		cpt := opts.charsPerToken
		if !(cpt > 0) || math.IsInf(cpt, 1) {
			return metrics.RawMetrics{}, fmt.Errorf("extracting %s: chars per token %v: must be positive and finite", p.id, cpt)
		}
		r.TokensEst = int(float64(srcBytes) / cpt)
		r.TokensEstWithTests = int(float64(allBytes) / cpt)
	}

	r.InternalImports, r.ExternalImports, r.StdlibImports = len(p.internal), len(p.external), len(p.stdlib)
	r.FanIn, r.FanInTests = len(p.fanIn), len(p.fanInTests)

	dup, err := duplication(p, r.SLOC, opts.dup)
	if err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", p.id, err)
	}
	r.DupBlocks, r.DuplicationPct = dup.Blocks, dup.Pct

	// Martin's package metrics: Ca = fan_in and Ce = internal_imports;
	// abstractness is exported interfaces over exported classes,
	// interfaces, type aliases and enums. Reported, not gated.
	r.Instability = metrics.Instability(r.FanIn, r.InternalImports)
	r.Abstractness = metrics.Abstractness(interfaces, types)
	r.MainSequenceDistance = metrics.MainSequenceDistance(r.Abstractness, r.Instability)

	locs := make([]string, 0, len(dup.Locations))
	for _, l := range dup.Locations {
		locs = append(locs, relLocation(p.dir, l))
	}
	m.setDetails(p.id, details{untested: untested, dupLocations: locs})
	return r, nil
}

// duplication runs the duplicate finder over the non-test files of p, with
// sloc the package's SLOC.
func duplication(p *pkg, sloc int, opts duptok.Options) (duptok.Result, error) {
	var s duptok.Stream
	for _, f := range p.src {
		t := &f.toks
		for i, c := range t.codes {
			if err := s.Add(c, t.class[i], int(t.line[i]), int(t.last[i])); err != nil {
				return duptok.Result{}, err
			}
		}
		s.EndFile(f.abs, f.codeLines)
	}
	return s.Count(opts, sloc)
}

// relLocation renders loc as "file:start-end" with file relative to dir, in
// slash form, falling back to the base name.
func relLocation(dir string, loc duptok.Location) string {
	file := filepath.Base(loc.File)
	if rel, err := filepath.Rel(dir, loc.File); err == nil {
		file = rel
	}
	return filepath.ToSlash(file) + ":" + strconv.Itoa(loc.StartLine) + "-" + strconv.Itoa(loc.EndLine)
}
