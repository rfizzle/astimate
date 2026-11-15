package rows

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// rankRows are rows with ties on every sort key, in no particular order;
// c has no churn, which sorts after b's 0.
func rankRows() []Row {
	churn := func(n int) *int { return &n }
	return []Row{
		{Path: "c", AgentPasses: 0.5, HumanDays: 2, HumanDaysLow: 2, FanIn: 1, TokensEst: 100, DuplicationPct: 0},
		{Path: "a", AgentPasses: 0.5, HumanDays: 1, HumanDaysLow: 1, FanIn: 3, TokensEst: 300, DuplicationPct: 10, ChurnCommits: churn(5)},
		{Path: "d", AgentPasses: 1.5, HumanDays: 1, HumanDaysLow: 1, FanIn: 1, TokensEst: 100, DuplicationPct: 10, ChurnCommits: churn(5)},
		{Path: "b", AgentPasses: 0.1, HumanDays: 2, HumanDaysLow: 2, FanIn: 0, TokensEst: 200, DuplicationPct: 30, ChurnCommits: churn(0)},
	}
}

func paths(rows []Row) []string {
	out := make([]string, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].Path)
	}
	return out
}

func TestSortRowsStableOrdering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		want []string
	}{
		{SortPasses, []string{"d", "a", "c", "b"}},
		{SortDays, []string{"b", "c", "a", "d"}},
		{SortFanIn, []string{"a", "c", "d", "b"}},
		{SortTokens, []string{"a", "b", "c", "d"}},
		{SortDuplication, []string{"b", "a", "d", "c"}},
		{SortChurn, []string{"a", "d", "b", "c"}},
		{SortHotspot, []string{"d", "a", "b", "c"}},
		// No row has a ceiling: all tie, in path order.
		{SortHeadroom, []string{"a", "b", "c", "d"}},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()

			// Every permutation of the input must give the same order.
			base := rankRows()
			for _, perm := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}, {1, 3, 0, 2}, {2, 0, 3, 1}} {
				rows := make([]Row, 0, len(base))
				for _, i := range perm {
					rows = append(rows, base[i])
				}
				if err := SortRows(rows, tt.key); err != nil {
					t.Fatalf("SortRows(%q) error = %v", tt.key, err)
				}
				if got := paths(rows); !slices.Equal(got, tt.want) {
					t.Errorf("SortRows(%q) from permutation %v = %q, want %q", tt.key, perm, got, tt.want)
				}
			}
		})
	}
}

func TestSortRowsUnknownKey(t *testing.T) {
	t.Parallel()

	rows := rankRows()
	err := SortRows(rows, "size")
	if !errors.Is(err, ErrUnknownSortKey) {
		t.Fatalf("SortRows(size) error = %v, want ErrUnknownSortKey", err)
	}
	if got, want := paths(rows), paths(rankRows()); !slices.Equal(got, want) {
		t.Errorf("rows after failed sort = %q, want unchanged %q", got, want)
	}
}

func TestNewRowWorkedExample(t *testing.T) {
	t.Parallel()

	m := workedExample()
	m.FanIn = 7
	got := NewRow("internal/billing", &m, params())
	want := Row{
		Path: "internal/billing", AgentPasses: 1.2, HumanDays: 71.6, HumanDaysLow: 71.6, HumanDaysHigh: 89.6, Tier: score.TierFewPasses,
		FanIn: 7, TokensEst: 10000, DuplicationPct: 20,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewRow = %+v, want %+v", got, want)
	}
}

func TestNewCeiling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		value, limit       float64
		wantRemain, wantUs float64
	}{
		{name: "under", value: 14100, limit: 16000, wantRemain: 1900, wantUs: 0.881},
		{name: "at the max", value: 60, limit: 60, wantRemain: 0, wantUs: 1},
		{name: "over", value: 16694, limit: 16000, wantRemain: -694, wantUs: 1.043},
		{name: "empty", value: 0, limit: 10, wantRemain: 10, wantUs: 0},
		{name: "fractional", value: 33.3, limit: 45, wantRemain: 11.7, wantUs: 0.74},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := NewCeiling("m", tt.value, tt.limit)
			want := Ceiling{Metric: "m", Value: tt.value, Max: tt.limit, Remaining: tt.wantRemain, Used: tt.wantUs}
			if got != want {
				t.Errorf("NewCeiling(%v, %v) = %+v, want %+v", tt.value, tt.limit, got, want)
			}
		})
	}
}

// TestSetCeilingsTightest: the headroom is the ceiling with the highest
// share used, the first in rule order on a tie, a copy of it, and nil
// with no ceiling.
func TestSetCeilingsTightest(t *testing.T) {
	t.Parallel()

	tokens := NewCeiling("tokens_est", 14100, 16000)  // 0.881
	exports := NewCeiling("exported_symbols", 30, 60) // 0.5
	imports := NewCeiling("internal_imports", 11, 10) // 1.1, over
	sloc := NewCeiling("sloc", 881, 1000)             // 0.881, ties tokens
	tests := []struct {
		name string
		cs   []Ceiling
		want *Ceiling
	}{
		{name: "none", cs: nil, want: nil},
		{name: "one", cs: []Ceiling{exports}, want: &exports},
		{name: "tightest last", cs: []Ceiling{exports, tokens}, want: &tokens},
		{name: "tightest first", cs: []Ceiling{tokens, exports}, want: &tokens},
		{name: "over a max wins", cs: []Ceiling{tokens, imports, exports}, want: &imports},
		{name: "tie takes the first", cs: []Ceiling{sloc, exports, tokens}, want: &sloc},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var r Row
			r.SetCeilings(tt.cs)
			if !reflect.DeepEqual(r.Headroom, tt.want) {
				t.Errorf("Headroom = %+v, want %+v", r.Headroom, tt.want)
			}
			if !slices.Equal(r.Ceilings, tt.cs) {
				t.Errorf("Ceilings = %+v, want %+v", r.Ceilings, tt.cs)
			}
			if r.Headroom != nil && len(r.Ceilings) > 0 {
				r.Headroom.Used = 99
				if slices.ContainsFunc(r.Ceilings, func(c Ceiling) bool { return c.Used == 99 }) {
					t.Error("Headroom aliases an element of Ceilings, want a copy")
				}
			}
		})
	}
}

// TestSortHeadroom: tightest first, a package over its max at the top,
// ties by path, and a package no capacity rule judges last.
func TestSortHeadroom(t *testing.T) {
	t.Parallel()

	withRoom := func(path string, cs ...Ceiling) Row {
		r := Row{Path: path}
		r.SetCeilings(cs)
		return r
	}
	base := []Row{
		{Path: "a-unjudged"},
		withRoom("roomy", NewCeiling("tokens_est", 1000, 16000)),
		withRoom("z-tied", NewCeiling("exported_symbols", 45, 60)),
		withRoom("over", NewCeiling("tokens_est", 17000, 16000)),
		withRoom("b-tied", NewCeiling("tokens_est", 12000, 16000)),
		withRoom("empty", NewCeiling("internal_imports", 0, 10)),
	}
	want := []string{"over", "b-tied", "z-tied", "roomy", "empty", "a-unjudged"}
	for _, perm := range [][]int{{0, 1, 2, 3, 4, 5}, {5, 4, 3, 2, 1, 0}, {2, 0, 5, 1, 3, 4}} {
		rs := make([]Row, 0, len(base))
		for _, i := range perm {
			rs = append(rs, base[i])
		}
		if err := SortRows(rs, SortHeadroom); err != nil {
			t.Fatal(err)
		}
		if got := paths(rs); !slices.Equal(got, want) {
			t.Errorf("SortRows(headroom) from permutation %v = %q, want %q", perm, got, want)
		}
	}
}

// TestWriteRowsJSONHeadroom: a row carries its headroom and ceilings; one
// no capacity rule judges has a null headroom and no ceilings key.
func TestWriteRowsJSONHeadroom(t *testing.T) {
	t.Parallel()

	judged := Row{Path: "big", Tier: score.TierOnePass}
	judged.SetCeilings([]Ceiling{NewCeiling("tokens_est", 14100, 16000), NewCeiling("exported_symbols", 12, 60)})
	var buf bytes.Buffer
	if err := WriteRowsJSON(&buf, []Row{judged, {Path: "cmd/app", Tier: score.TierOnePass, Role: "main"}}); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decoding %s: %v", buf.String(), err)
	}
	wantHeadroom := map[string]any{"metric": "tokens_est", "value": 14100.0, "max": 16000.0, "remaining": 1900.0, "used": 0.881}
	if !reflect.DeepEqual(got[0]["headroom"], wantHeadroom) {
		t.Errorf("headroom = %v, want %v", got[0]["headroom"], wantHeadroom)
	}
	wantCeilings := []any{
		wantHeadroom,
		map[string]any{"metric": "exported_symbols", "value": 12.0, "max": 60.0, "remaining": 48.0, "used": 0.2},
	}
	if !reflect.DeepEqual(got[0]["ceilings"], wantCeilings) {
		t.Errorf("ceilings = %v, want %v", got[0]["ceilings"], wantCeilings)
	}
	if h, ok := got[1]["headroom"]; !ok || h != nil {
		t.Errorf("unjudged row headroom = %v (present %v), want null", h, ok)
	}
	if _, ok := got[1]["ceilings"]; ok {
		t.Errorf("unjudged row = %v, want no ceilings key", got[1])
	}
	if !strings.Contains(buf.String(), `"headroom": {`+"\n"+`      "metric": "tokens_est",`+"\n"+`      "value": 14100,`) {
		t.Errorf("JSON = %s, want integer values written without a fraction", buf.String())
	}
}

func TestWriteRowsTableHeadroom(t *testing.T) {
	t.Parallel()

	tight := Row{Path: "internal/big", AgentPasses: 1.2, HumanDays: 2, HumanDaysLow: 2, HumanDaysHigh: 2, Tier: score.TierFewPasses, TokensEst: 14100, Role: "library"}
	tight.SetCeilings([]Ceiling{NewCeiling("tokens_est", 14100, 16000)})
	over := Row{Path: "x", HumanDays: 1, HumanDaysLow: 1, HumanDaysHigh: 1, Tier: score.TierOnePass, TokensEst: 17250, Role: "library"}
	over.SetCeilings([]Ceiling{NewCeiling("tokens_est", 17250, 16000)})
	rs := []Row{over, tight, {Path: "cmd/app", Tier: score.TierOnePass, Role: "main"}}
	var buf bytes.Buffer
	if err := WriteRowsTable(&buf, rs, TableOptions{Headroom: true}); err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"PATH          PASSES  TIER        DAYS  FAN_IN  TOKENS  DUP%  CHURN  ROLE     HEADROOM",
		"x                0.0  ONE_PASS     1.0       0   17250   0.0      -  library  tokens_est 17,250/16,000 (1,250 over)",
		"internal/big     1.2  FEW_PASSES   2.0       0   14100   0.0      -  library  tokens_est 14,100/16,000 (1,900 left)",
		"cmd/app          0.0  ONE_PASS     0.0       0       0   0.0      -  main     -",
		"",
	}, "\n")
	if got := buf.String(); got != want {
		t.Errorf("WriteRowsTable with headroom =\n%s\nwant:\n%s", got, want)
	}
}

func TestGrouped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   float64
		want string
	}{
		{0, "0"}, {12, "12"}, {999, "999"}, {1000, "1,000"}, {16000, "16,000"},
		{1234567, "1,234,567"}, {-1250, "-1,250"}, {6.7, "6.7"}, {12345.25, "12,345.25"},
	}
	for _, tt := range tests {
		if got := grouped(tt.in); got != tt.want {
			t.Errorf("grouped(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWriteRowsJSONEmpty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WriteRowsJSON(&buf, nil); err != nil {
		t.Fatalf("WriteRowsJSON error = %v", err)
	}
	if got := buf.String(); got != "[]\n" {
		t.Errorf("WriteRowsJSON(nil) = %q, want %q", got, "[]\n")
	}
}

func TestWriteRowsTable(t *testing.T) {
	t.Parallel()

	billingChurn := 42
	rows := []Row{
		{Path: "internal/billing", AgentPasses: 1.2, HumanDays: 55.2, HumanDaysLow: 41.3, HumanDaysHigh: 55.2, Tier: score.TierFewPasses,
			FanIn: 7, TokensEst: 10000, DuplicationPct: 20, ChurnCommits: &billingChurn, Role: "library"},
		{Path: "cmd/app", AgentPasses: 0.1, HumanDays: 2.5, HumanDaysLow: 2.46, HumanDaysHigh: 2.5, Tier: score.TierOnePass, TokensEst: 900, Role: "main"},
		{Path: ".", AgentPasses: 0, HumanDays: 0.3, HumanDaysLow: 0.3, HumanDaysHigh: 0.3, Tier: score.TierOnePass, TokensEst: 55},
	}
	var buf bytes.Buffer
	if err := WriteRowsTable(&buf, rows, TableOptions{}); err != nil {
		t.Fatalf("WriteRowsTable error = %v", err)
	}
	// A row whose extractor records no role, or without churn, shows a
	// dash; a range whose ends round the same shows once; DAYS follows
	// the passes and the tier.
	want := strings.Join([]string{
		"PATH              PASSES  TIER             DAYS  FAN_IN  TOKENS  DUP%  CHURN  ROLE",
		"internal/billing     1.2  FEW_PASSES  41.3-55.2       7   10000  20.0     42  library",
		"cmd/app              0.1  ONE_PASS          2.5       0     900   0.0      -  main",
		".                    0.0  ONE_PASS          0.3       0      55   0.0      -  -",
		"",
	}, "\n")
	if got := buf.String(); got != want {
		t.Errorf("WriteRowsTable =\n%s\nwant:\n%s", got, want)
	}
}

func params() score.RebuildParams {
	return score.RebuildParams{
		ContextBudget: 25000, TokensPerExport: 40, TokensPerUntestedExport: 800, TokensPerHiddenState: 400,
		SuperlinearExponent: 1.3, CocomoA: 2.4, CocomoB: 1.05, DaysPerMonth: 19,
		Cocomo: score.Cocomo{Model: score.ModelBasic81, UNFM: "completely_unfamiliar"}, Tiers: score.Tiers{OnePassMax: 1, FewPassesMax: 3},
	}
}

func workedExample() metrics.RawMetrics {
	return metrics.RawMetrics{
		SLOC: 1200, TokensEst: 10000, TokensEstWithTests: 16000, DuplicationPct: 20,
		ExportedSymbols: 30, UntestedExports: 15, Globals: 2, InitFuncs: 1,
	}
}

func TestHotspot(t *testing.T) {
	t.Parallel()

	churn := 41
	if got := Hotspot(&churn, 8.04); got != 329.6 {
		t.Errorf("Hotspot = %v, want 329.6", got)
	}
	if got := Hotspot(nil, 8.04); got != 0 {
		t.Errorf("Hotspot without churn = %v, want 0", got)
	}
}

// TestSortHotspotDisagreesWithDays: by churn times human days "wide" would
// lead, by churn times agent passes "deep" does; rows with no hotspot
// follow by the low end of human days, then path.
func TestSortHotspotDisagreesWithDays(t *testing.T) {
	t.Parallel()

	five, three := 5, 3
	rows := []Row{
		{Path: "idle-b", HumanDaysLow: 2},
		{Path: "wide", AgentPasses: 1, HumanDaysLow: 10, ChurnCommits: &five},
		{Path: "idle-a", HumanDaysLow: 2},
		{Path: "deep", AgentPasses: 4, HumanDaysLow: 8, ChurnCommits: &three},
		{Path: "idle-big", HumanDays: 1, HumanDaysLow: 9},
	}
	if err := SortRows(rows, SortHotspot); err != nil {
		t.Fatal(err)
	}
	want := []string{"deep", "wide", "idle-big", "idle-a", "idle-b"}
	if got := paths(rows); !slices.Equal(got, want) {
		t.Errorf("order = %q, want %q", got, want)
	}
}
