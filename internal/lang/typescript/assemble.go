package typescript

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

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
	var candidates []candidate
	var globalPos []metrics.Position
	files := make([]string, 0, len(p.src))
	largest := ""
	inits := 0
	for _, f := range p.src {
		rel := relFile(p.dir, f.abs)
		files = append(files, rel)
		r.Files++
		r.SLOC += f.sloc
		if largest == "" || f.sloc > r.LargestFileSLOC {
			largest, r.LargestFileSLOC = rel, f.sloc
		}
		srcBytes += f.size
		srcTokens += f.o200k
		r.ExportedSymbols += f.exports
		types += f.exportedTypes
		interfaces += f.exportedInterfaces
		r.Globals += len(f.globals)
		for _, line := range f.globals {
			globalPos = append(globalPos, metrics.Position{File: rel, Line: line})
		}
		if f.hasInit {
			inits++
		}
		for _, fn := range f.funcs {
			scores = append(scores, fn.cognitive)
			r.CognitiveTotal += fn.cognitive
			r.MaxNesting = max(r.MaxNesting, fn.nesting)
		}
		for _, c := range f.exportedFuncs {
			candidates = append(candidates, candidate{exportedFunc: c, file: rel})
		}
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
	var excluded []string
	var missed []candidate
	for _, c := range candidates {
		switch {
		case c.directed:
			excluded = append(excluded, c.display)
		case !referenced[c.match]:
			missed = append(missed, c)
		}
	}
	slices.SortStableFunc(missed, func(a, b candidate) int { return strings.Compare(a.display, b.display) })
	var untested []string
	var untestedPos []metrics.Position
	if len(missed) > 0 {
		untested = make([]string, len(missed))
		untestedPos = make([]metrics.Position, len(missed))
		for i, c := range missed {
			untested[i] = c.display
			untestedPos[i] = metrics.Position{File: c.file, Line: c.line}
		}
	}
	slices.Sort(excluded)
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
	// interfaces, type aliases and enums. Reported, not gated. Each ratio
	// is rounded to three decimal places, the distance computed from the
	// unrounded ratios, so none carries float noise.
	instability := metrics.Instability(r.FanIn, r.InternalImports)
	abstractness := metrics.Abstractness(interfaces, types)
	r.MainSequenceDistance = metrics.RoundRatio(metrics.MainSequenceDistance(abstractness, instability))
	r.Instability = metrics.RoundRatio(instability)
	r.Abstractness = metrics.RoundRatio(abstractness)

	locs := make([]string, 0, len(dup.Locations))
	for _, l := range dup.Locations {
		locs = append(locs, relLocation(p.dir, l))
	}
	slices.Sort(files)
	m.setDetails(p.id, details{
		untested:     untested,
		excluded:     excluded,
		dupLocations: locs,
		untestedPos:  untestedPos,
		globalPos:    globalPos,
		largestFile:  largest,
		sourceFiles:  files,
	})
	return r, nil
}

// candidate is an untested_exports candidate of a package with the file,
// relative to the package directory, that declares it.
type candidate struct {
	exportedFunc
	file string
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
	return relFile(dir, loc.File) + ":" + strconv.Itoa(loc.StartLine) + "-" + strconv.Itoa(loc.EndLine)
}

// relFile returns file relative to dir in slash form, falling back to the
// base name when it cannot be related to dir.
func relFile(dir, file string) string {
	rel := filepath.Base(file)
	if r, err := filepath.Rel(dir, file); err == nil {
		rel = r
	}
	return filepath.ToSlash(rel)
}
