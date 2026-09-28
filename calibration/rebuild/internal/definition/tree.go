package definition

import (
	"math"
	"reflect"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// Units: what one experiment rebuilds.
const (
	// UnitPackage rebuilds one package; it is the unit of a definition
	// that names none.
	UnitPackage = "package"
	// UnitTree rebuilds a directory's package and every non-main package
	// below it together.
	UnitTree = "tree"
)

// EstimateSum is the one estimate method of a tree: the members'
// rebuild_tokens summed, and the sum passed through the SPEC.md 7.2 curve
// once.
const EstimateSum = "sum"

// minTrees and maxTrees bound the size of a tree definition.
const (
	minTrees = 12
	maxTrees = 16
)

// UnitOrDefault returns the definition's unit, UnitPackage when it names
// none.
func (d *Definition) UnitOrDefault() string {
	if d.Unit == "" {
		return UnitPackage
	}
	return d.Unit
}

// Member is one package of a tree experiment, with its own estimate and
// metrics at the pin.
type Member struct {
	// Package is the import path.
	Package string `yaml:"package"`
	// Dir is the package directory relative to the module root.
	Dir string `yaml:"dir"`
	// HasTests is the package's own has_tests.
	HasTests bool `yaml:"has_tests"`
	// Tier, AgentPasses, RebuildTokens and HumanDays are the package's
	// own estimate before the run, rounded as for an experiment.
	Tier          score.Tier `yaml:"tier"`
	AgentPasses   float64    `yaml:"agent_passes"`
	RebuildTokens int        `yaml:"rebuild_tokens"`
	HumanDays     float64    `yaml:"human_days"`
	// Metrics are the package's raw metrics at the pin.
	Metrics Metrics `yaml:"metrics"`
}

// Aggregate combines the members' metrics into the tree's: counts and
// sizes are summed; largest_file_sloc, max_nesting and cognitive_p90 are
// the members' largest; duplication_pct is weighted by tokens_est, so the
// volume term of the aggregate is the sum of the members'; has_tests,
// uses_cgo and uses_reflect hold when they hold for any member; and the
// module-level and ratio fields (dup_blocks_cross_pkg, instability,
// abstractness, main_sequence_distance, coverage_pct,
// changed_func_cognitive_max) are null.
func Aggregate(members []Member) metrics.RawMetrics {
	var a metrics.RawMetrics
	var volume float64
	cgo, refl, gen, genTokens := false, false, 0, 0
	for i := range members {
		m := &members[i].Metrics.RawMetrics
		addCounts(&a, m)
		volume += float64(m.TokensEst) * (1 - m.DuplicationPct/100)
		a.HasTests = a.HasTests || m.HasTests
		cgo = cgo || (m.UsesCgo != nil && *m.UsesCgo)
		refl = refl || (m.UsesReflect != nil && *m.UsesReflect)
		if m.GeneratedFiles != nil {
			gen += *m.GeneratedFiles
		}
		if m.TokensEstGenerated != nil {
			genTokens += *m.TokensEstGenerated
		}
	}
	if a.TokensEst > 0 {
		a.DuplicationPct = max(0, 100*(1-volume/float64(a.TokensEst)))
	}
	a.UsesCgo, a.UsesReflect, a.GeneratedFiles, a.TokensEstGenerated = &cgo, &refl, &gen, &genTokens
	return a
}

// addCounts adds every int field of m to a's, taking the larger value
// instead for the per-package maxima (largest_file_sloc, max_nesting,
// cognitive_p90). The int fields of RawMetrics are exactly its counts and
// sizes, so a metric added later is summed without a change here.
func addCounts(a, m *metrics.RawMetrics) {
	va, vm := reflect.ValueOf(a).Elem(), reflect.ValueOf(m).Elem()
	for i := range va.NumField() {
		f := va.Field(i)
		if f.Kind() != reflect.Int {
			continue
		}
		switch va.Type().Field(i).Name {
		case "LargestFileSLOC", "MaxNesting", "CognitiveP90":
			f.SetInt(max(f.Int(), vm.Field(i).Int()))
		default:
			f.SetInt(f.Int() + vm.Field(i).Int())
		}
	}
}

// InTree reports whether the module-relative directory p is the tree
// directory dir or below it.
func InTree(dir, p string) bool {
	return p == dir || strings.HasPrefix(p, dir+"/")
}

// TreePattern is the go package pattern of the tree at the module-relative
// directory dir: "./dir/...".
func TreePattern(dir string) string {
	return OwnPattern(dir) + "/..."
}

// validateTrees checks what a tree definition needs beyond valid
// experiments: the estimate method, the tree bounds of the selection rule,
// minTrees to maxTrees experiments, at least half of them tested, at most
// max_per_module from one module, and valid members.
func (d *Definition) validateTrees() []error {
	p := &problems{prefix: invalid}
	fail := p.add
	if d.EstimateMethod != EstimateSum {
		fail("estimate_method %q is not %q", d.EstimateMethod, EstimateSum)
	}
	r := d.Selection
	if r.MinPackages < 2 || r.MaxPackages < r.MinPackages {
		fail("selection min_packages must be >= 2 and max_packages >= min_packages")
	}
	if n := len(d.Experiments); n < minTrees || n > maxTrees {
		fail("want %d to %d trees, got %d", minTrees, maxTrees, n)
	}
	tested := 0
	perModule := map[string]int{}
	for i := range d.Experiments {
		e := &d.Experiments[i]
		if e.HasTests {
			tested++
		}
		if perModule[e.Module]++; perModule[e.Module] > r.MaxPerModule {
			fail("module %s has more than max_per_module %d trees", e.Module, r.MaxPerModule)
		}
		for _, err := range e.validateTree(r.MinPackages, r.MaxPackages) {
			fail("experiments[%d] %s: %w", i, e.Package, err)
		}
	}
	if 2*tested < len(d.Experiments) {
		fail("only %d of %d trees are tested; want at least half", tested, len(d.Experiments))
	}
	return p.errs
}

// validateTree checks a tree experiment: minPkgs to maxPkgs members, the
// first the tree's own package and the rest below it in directory order,
// each valid; has_tests, rebuild_tokens, human_days and metrics derived
// from the members; and an oracle testing the tree when it has tests and
// importers outside it when it has none.
func (e *Experiment) validateTree(minPkgs, maxPkgs int) []error {
	p := &problems{}
	fail := p.add
	if e.Dir == "." {
		fail("a tree cannot be the whole module")
	}
	if n := len(e.Members); n < minPkgs || n > maxPkgs {
		fail("want %d to %d members, got %d", minPkgs, maxPkgs, n)
	}
	if len(e.Members) > 0 && (e.Members[0].Package != e.Package || e.Members[0].Dir != e.Dir) {
		fail("the first member must be the tree's own package %s", e.Package)
	}
	tested, tokens, days := false, 0, 0.0
	for i := range e.Members {
		m := &e.Members[i]
		tested = tested || m.HasTests
		tokens += m.RebuildTokens
		days += m.HumanDays
		switch {
		case !InTree(e.Dir, m.Dir) || m.Dir != ModRelDir(e.Module, m.Package):
			fail("member %s (dir %q) is not below %s", m.Package, m.Dir, e.Dir)
		case i > 0 && m.Dir <= e.Members[i-1].Dir:
			fail("members are not sorted by directory at %s", m.Dir)
		case m.HasTests != m.Metrics.HasTests:
			fail("member %s has_tests disagrees with its metrics", m.Package)
		case m.RebuildTokens <= 0 || m.AgentPasses < 0 || m.HumanDays < 0:
			fail("member %s: rebuild_tokens must be > 0 and agent_passes and human_days >= 0", m.Package)
		}
		if err := m.Metrics.Validate(); err != nil {
			fail("member %s: %w", m.Package, err)
		}
		if m.Metrics.UsesCgo == nil || *m.Metrics.UsesCgo {
			fail("member %s: uses_cgo must be false", m.Package)
		}
	}
	if len(e.Members) == 0 {
		return p.errs
	}
	switch {
	case e.HasTests != tested:
		fail("has_tests %v, want %v (any member tested)", e.HasTests, tested)
	case e.RebuildTokens != tokens:
		fail("rebuild_tokens %d is not the members' sum %d", e.RebuildTokens, tokens)
	case e.HumanDays != math.Round(days*10)/10:
		fail("human_days %v is not the members' sum %v", e.HumanDays, math.Round(days*10)/10)
	case !reflect.DeepEqual(e.Metrics.RawMetrics, Aggregate(e.Members)):
		fail("metrics are not the aggregate of the members'")
	}
	switch {
	case tested && !slices.Equal(e.Oracle.Test, []string{TreePattern(e.Dir)}):
		fail("oracle.test of a tested tree must be [%s], got %v", TreePattern(e.Dir), e.Oracle.Test)
	case !tested && slices.ContainsFunc(e.Oracle.Test, func(p string) bool { return InTree(OwnPattern(e.Dir), p) }):
		fail("oracle.test of an untested tree must list importers outside it, got %v", e.Oracle.Test)
	}
	return p.errs
}
