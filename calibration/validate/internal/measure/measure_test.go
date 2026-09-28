package measure

import (
	"math"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/replay/labels"
	"github.com/rfizzle/astimate/calibration/validate/internal/corpus"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// defaultRules returns the embedded default's rules.
func defaultRules(t *testing.T) *Rules {
	t.Helper()
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	return NewRules(cfg)
}

// index returns the List index of the rule named name.
func index(t *testing.T, s *Rules, name string) int {
	t.Helper()
	for i, r := range s.List {
		if r.Name() == name {
			return i
		}
	}
	t.Fatalf("no rule %s", name)
	return -1
}

// row returns a Go package row with sloc head and base (base < 0: new)
// and the violations the replay would have recorded under the default.
func row(head, base int) corpus.Row {
	r := corpus.Row{Package: "p", Language: "go", Head: metrics.RawMetrics{SLOC: head, HasTests: true}}
	if base >= 0 {
		r.Base = &metrics.RawMetrics{SLOC: base, HasTests: true}
		r.SLOCDelta = head - base
	} else {
		r.SLOCDelta = head
	}
	if head > 1000 && (base < 0 || head > base) {
		r.Violated = []string{"sloc"}
	}
	return r
}

// commit returns a commit with the given verdict and rows.
func commit(hash string, block bool, rows ...corpus.Row) corpus.Commit {
	v := labels.Allow
	if block {
		v = labels.Block
	}
	return corpus.Commit{Hash: hash, Subject: "s " + hash, Label: labels.Label{Hash: hash, Verdict: v, Reason: "r"}, Rows: rows}
}

// planted builds a corpus of 10 block and 20 allow commits in which the
// sloc rule fires on 8 block and 4 allow commits: 8 of 10 recall, 4 of 20
// false failures, precision 8 of 12.
func planted() *corpus.Corpus {
	c := &corpus.Corpus{Name: "planted"}
	for i := range 10 {
		head := 900
		if i < 8 {
			head = 1200
		}
		c.Commits = append(c.Commits, commit("b"+string(rune('a'+i)), true, row(head, 800)))
	}
	for i := range 20 {
		head := 950
		if i < 4 {
			head = 1100
		}
		c.Commits = append(c.Commits, commit("a"+string(rune('a'+i)), false, row(head, 900)))
	}
	return c
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestPlantedRates(t *testing.T) {
	s := defaultRules(t)
	sc, err := Score(planted(), s)
	if err != nil {
		t.Fatal(err)
	}
	g := sc.Gate()
	if g != (Rates{Block: 10, Allow: 20, BlockFailed: 8, AllowFailed: 4}) {
		t.Fatalf("gate rates %+v", g)
	}
	sl := sc.Rule(index(t, s, "sloc"))
	p, ok := sl.Precision()
	if !ok || !near(p, 8.0/12) || !near(sl.Recall(), 0.8) || !near(sl.FalseFailure(), 0.2) || !near(sl.J(), 0.6) {
		t.Errorf("sloc precision %v recall %v false %v", p, sl.Recall(), sl.FalseFailure())
	}
	if r := sc.Rule(index(t, s, "globals")); r.Failed() != 0 {
		t.Errorf("globals fired on %d commits", r.Failed())
	}
	if _, ok := sc.Rule(index(t, s, "globals")).Precision(); ok {
		t.Error("precision of a rule that never fired is defined")
	}
	if checked, bad := sc.Recheck(); bad != 0 || checked == 0 {
		t.Errorf("recheck %d of %d", bad, checked)
	}
	if n := sc.Judged(index(t, s, "sloc")); n != 30 {
		t.Errorf("sloc judged %d commits", n)
	}
	if n := sc.Judged(index(t, s, "dup_blocks_cross_pkg")); n != 0 {
		t.Errorf("module rule judged %d commits without a module row", n)
	}
	if m := sc.Missed(); len(m) != 2 || m[0].Hash != "bi" {
		t.Errorf("missed %v", m)
	}
	// Block commits grew by 400 or 100 SLOC, allow commits by 200 or 50:
	// the size-only rule at 250 fails 8 block commits and no allow commit,
	// at 150 the same 8 and 4 allow commits.
	if r := sc.Size(250); r.BlockFailed != 8 || r.AllowFailed != 0 {
		t.Errorf("size 250 %+v", r)
	}
	if r := sc.Size(150); r.BlockFailed != 8 || r.AllowFailed != 4 {
		t.Errorf("size 150 %+v", r)
	}
}

func TestScoreRejectsUngatedViolation(t *testing.T) {
	c := &corpus.Corpus{Commits: []corpus.Commit{commit("x", true, corpus.Row{Package: "p", Language: "go", Violated: []string{"dup_blocks_cross_pkg"}})}}
	if _, err := Score(c, defaultRules(t)); err == nil || !strings.Contains(err.Error(), "no rule gates") {
		t.Errorf("err = %v", err)
	}
}

func TestSweepCapacity(t *testing.T) {
	s := defaultRules(t)
	c := &corpus.Corpus{Commits: []corpus.Commit{
		commit("new", true, row(1200, -1)),     // new over max: fires
		commit("cross", true, row(1100, 900)),  // crossed: fires
		commit("grow", false, row(1300, 1200)), // grew over: fires
		commit("fell", false, row(1200, 1300)), // fell, still over: passes
	}}
	sc, err := Score(c, s)
	if err != nil {
		t.Fatal(err)
	}
	i := index(t, s, "sloc")
	curves := sc.Sweep(i)
	if len(curves) != 1 || curves[0].Param != Max {
		t.Fatalf("curves %+v", curves)
	}
	got := map[float64][2]int{}
	for _, p := range curves[0].Points {
		got[p.Setting.Value] = [2]int{p.Rule.BlockFailed, p.Rule.AllowFailed}
		if p.Shipped != (p.Setting.Value == 1000) {
			t.Errorf("shipped marks %v", p.Setting)
		}
	}
	want := map[float64][2]int{500: {2, 1}, 1000: {2, 1}, 1250: {0, 1}, 1500: {0, 0}}
	for v, w := range want {
		if got[v] != w {
			t.Errorf("max %v: got %v want %v", v, got[v], w)
		}
	}
	rule, all := sc.Swap(i, nil)
	if rule.Failed() != 0 || all.Failed() != 0 {
		t.Errorf("dropped rule still fails: %+v %+v", rule, all)
	}
	fired, over := sc.Legacy(i)
	if fired.Failed() != 3 || over.Failed() != 1 || over.AllowFailed != 1 {
		t.Errorf("legacy fired %+v over %+v", fired, over)
	}
}

func TestSweepDensityAndModule(t *testing.T) {
	s := defaultRules(t)
	one, zero, five := 1, 0, 5
	dup := func(head int, base *int) corpus.Row {
		r := corpus.Row{Package: "q", Language: "go", Head: metrics.RawMetrics{DupBlocks: head, HasTests: true}}
		if base != nil {
			r.Base = &metrics.RawMetrics{DupBlocks: *base, HasTests: true}
		}
		return r
	}
	mod := func(head int, base *int) corpus.Row {
		h := head
		r := corpus.Row{Package: metrics.ModuleRowID, Language: "go", Head: metrics.RawMetrics{DupBlocksCrossPkg: &h}}
		if base != nil {
			r.Base = &metrics.RawMetrics{DupBlocksCrossPkg: base}
		}
		return r
	}
	// A package row carrying a cross-package count is not judged by the
	// module rule; only the module row is.
	pkgCross := corpus.Row{Package: "r", Language: "go", Head: metrics.RawMetrics{DupBlocksCrossPkg: &five, HasTests: true}}
	c := &corpus.Corpus{Commits: []corpus.Commit{
		commit("a", true, dup(2, nil)),                 // new, ratchet_from_zero: +2
		commit("b", true, dup(3, &one)),                // +2
		commit("c", false, dup(2, &one)),               // +1
		commit("d", false, mod(3, &zero), pkgCross),    // module +3
		commit("e", false, mod(500, nil), pkgCross),    // new module row: only max 450
		commit("f", true, mod(2, &five), dup(1, &one)), // fell; no change
	}}
	sc, err := Score(c, s)
	if err != nil {
		t.Fatal(err)
	}
	fires := func(name string, set Setting) (int, int) {
		t.Helper()
		i := index(t, s, name)
		th := Apply(s.List[i].Threshold, set)
		r, _ := sc.Swap(i, &th)
		return r.BlockFailed, r.AllowFailed
	}
	cases := []struct {
		rule       string
		set        Setting
		block, all int
	}{
		{"dup_blocks", Setting{Param: MaxDelta, Value: 0}, 2, 1},
		{"dup_blocks", Setting{Param: MaxDelta, Value: 1}, 2, 0},
		{"dup_blocks", Setting{Param: MaxDelta, Value: 2}, 0, 0},
		{"dup_blocks_cross_pkg", Setting{Param: MaxDelta, Value: 0}, 0, 2},
		{"dup_blocks_cross_pkg", Setting{Param: MaxDelta, Value: 3}, 0, 1},
		{"dup_blocks_cross_pkg", Setting{Param: Max, Value: 600}, 0, 1},
		{"dup_blocks_cross_pkg", Setting{Param: MaxDelta, None: true}, 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.rule+" "+tc.set.String(), func(t *testing.T) {
			if b, a := fires(tc.rule, tc.set); b != tc.block || a != tc.all {
				t.Errorf("got %d block, %d allow; want %d, %d", b, a, tc.block, tc.all)
			}
		})
	}
	with := sc.With(map[int]*gate.Threshold{index(t, s, "dup_blocks"): nil})
	if with.BlockFailed != 0 {
		t.Errorf("with dup_blocks dropped the gate still fails %d block commits", with.BlockFailed)
	}
}

func TestRulesLanguageOverride(t *testing.T) {
	s := defaultRules(t)
	ts := corpus.Row{Package: "web", Language: "typescript"}
	i, ok := s.For(&ts, "sloc")
	if !ok || s.List[i].Language != "typescript" || s.List[i].Name() != "sloc (typescript)" {
		t.Fatalf("typescript sloc rule %v %v", i, ok)
	}
	j, ok := s.For(&ts, "dup_blocks")
	if !ok || s.List[j].Language != "" {
		t.Errorf("typescript dup_blocks should be the top-level rule")
	}
	goRow := corpus.Row{Package: "p", Language: "go"}
	if s.Judges(i, &goRow) {
		t.Error("the typescript override judges a Go row")
	}
	if _, ok := s.For(&goRow, "dup_blocks_cross_pkg"); ok {
		t.Error("a package row is judged by the module-wide rule")
	}
	if _, ok := s.For(&goRow, "fan_in"); ok {
		t.Error("an ungated metric has a rule")
	}
}

func TestGridAndApply(t *testing.T) {
	s := defaultRules(t)
	cases := []struct {
		rule  string
		param Param
		want  string
	}{
		{"sloc", Max, "max 500,max 750,max 1000,max 1250,max 1500,max 2000,max 3000,max 5000"},
		{"dup_blocks", MaxDelta, "max_delta 0,max_delta 1,max_delta 2,max_delta 3,max_delta 5,max_delta 10,max_delta 20"},
		{"cognitive_p90", MaxDelta, "max_delta 0,max_delta 1,max_delta 2,max_delta 3,max_delta 5,max_delta 6,max_delta 10,max_delta 20,max_delta none"},
		{"has_tests", When, "when > 0,when > 50,when > 100,when > 200,when > 500,when > 1000,no guard"},
	}
	for _, tc := range cases {
		t.Run(tc.rule, func(t *testing.T) {
			th := s.List[index(t, s, tc.rule)].Threshold
			var got []string
			for _, set := range Grid(th, tc.param) {
				got = append(got, set.String())
				applied := Apply(th, set)
				if back := Params(applied); !set.None && len(back) != len(Params(th)) {
					t.Errorf("apply %v changed the params: %v", set, back)
				}
			}
			if strings.Join(got, ",") != tc.want {
				t.Errorf("grid %s", strings.Join(got, ","))
			}
		})
	}
	nest := s.List[index(t, s, "max_nesting")].Threshold
	if p := Params(nest); len(p) != 2 || p[0] != Max || p[1] != MaxDelta {
		t.Errorf("params %v", p)
	}
	noMax := Apply(nest, Setting{Param: Max, None: true})
	if noMax.Max != nil || *nest.Max != 5 {
		t.Error("apply none must clear the copy only")
	}
	guard := Apply(s.List[index(t, s, "has_tests")].Threshold, Setting{Param: When, Value: 7})
	if guard.When.Metric != "sloc" || guard.When.Value != 7 {
		t.Errorf("guard %+v", guard.When)
	}
}

func TestAdvise(t *testing.T) {
	cr := Criteria{MinFired: 10, Budget: 0.1, MinJ: 0.02}
	pt := func(v float64, shipped bool, b, a int) Point {
		return Point{Setting: Setting{Param: Max, Value: v}, Shipped: shipped, Rule: Rates{Block: 100, Allow: 100, BlockFailed: b, AllowFailed: a}}
	}
	curve := func(ps ...Point) []Curve { return []Curve{{Param: Max, Points: ps}} }
	cases := []struct {
		name    string
		shipped Rates
		judged  int
		curves  []Curve
		action  Action
		to      float64
		why     string
	}{
		{"no rows", Rates{}, 0, nil, Keep, 0, "no row"},
		{"no limit", Rates{Block: 100, Allow: 100, BlockFailed: 30, AllowFailed: 30}, 50, nil, Keep, 0, "no limit"},
		{"thin", Rates{Block: 100, Allow: 100, BlockFailed: 3, AllowFailed: 2}, 5, curve(pt(1, true, 3, 2)), Keep, 0, "too thin"},
		{"none in budget", Rates{Block: 100, Allow: 100, BlockFailed: 60, AllowFailed: 40},
			50, curve(pt(1, true, 60, 40), pt(2, false, 50, 30)), Drop, 0, "least: 30.0% at max 2"},
		{"low J", Rates{Block: 100, Allow: 100, BlockFailed: 20, AllowFailed: 20},
			50, curve(pt(1, true, 20, 20), pt(2, false, 9, 8)), Drop, 0, "under J +2.0 pts"},
		{"retune", Rates{Block: 100, Allow: 100, BlockFailed: 60, AllowFailed: 30},
			50, curve(pt(1, true, 60, 30), pt(2, false, 40, 10), pt(3, false, 40, 9), pt(4, false, 20, 2)), Retune, 3, "max 3 fails 40.0%"},
		{"keep", Rates{Block: 100, Allow: 100, BlockFailed: 30, AllowFailed: 5},
			50, curve(pt(0.5, false, 50, 9), pt(1, true, 30, 5)), Keep, 0, "within 10.0%"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := Advise(tc.shipped, tc.judged, tc.curves, cr)
			if a.Action != tc.action || (a.Action == Retune && a.To.Value != tc.to) || !strings.Contains(a.Why, tc.why) {
				t.Errorf("got %+v", a)
			}
		})
	}
}

func TestBestSize(t *testing.T) {
	pts := []SizePoint{
		{0, Rates{Block: 10, Allow: 10, BlockFailed: 10, AllowFailed: 8}},
		{50, Rates{Block: 10, Allow: 10, BlockFailed: 6, AllowFailed: 1}},
		{100, Rates{Block: 10, Allow: 10, BlockFailed: 6, AllowFailed: 0}},
		{200, Rates{Block: 10, Allow: 10, BlockFailed: 2, AllowFailed: 0}},
	}
	if best, met := BestSize(pts, 0.1); !met || best.T != 100 {
		t.Errorf("best %v met %v", best.T, met)
	}
	if best, met := BestSize(pts[:1], 0.1); met || best.T != 0 {
		t.Errorf("fallback %v met %v", best.T, met)
	}
	var sc Scored
	sc.Corpus = &corpus.Corpus{Commits: []corpus.Commit{commit("x", true, row(300, 0))}}
	if got := sc.SizeCurve([]int{100, 400}); got[0].Rates.BlockFailed != 1 || got[1].Rates.BlockFailed != 0 {
		t.Errorf("size curve %+v", got)
	}
}
