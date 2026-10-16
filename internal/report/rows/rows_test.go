package rows

import (
	"bytes"
	"errors"
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
		{Path: "c", AgentPasses: 0.5, HumanDays: 2, FanIn: 1, TokensEst: 100, DuplicationPct: 0},
		{Path: "a", AgentPasses: 0.5, HumanDays: 1, FanIn: 3, TokensEst: 300, DuplicationPct: 10, ChurnCommits: churn(5)},
		{Path: "d", AgentPasses: 1.5, HumanDays: 1, FanIn: 1, TokensEst: 100, DuplicationPct: 10, ChurnCommits: churn(5)},
		{Path: "b", AgentPasses: 0.1, HumanDays: 2, FanIn: 0, TokensEst: 200, DuplicationPct: 30, ChurnCommits: churn(0)},
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
		Path: "internal/billing", AgentPasses: 1.2, HumanDays: 55.2, Tier: score.TierFewPasses,
		FanIn: 7, TokensEst: 10000, DuplicationPct: 20,
	}
	if got != want {
		t.Errorf("NewRow = %+v, want %+v", got, want)
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
		{Path: "internal/billing", AgentPasses: 1.2, HumanDays: 55.2, Tier: score.TierFewPasses,
			FanIn: 7, TokensEst: 10000, DuplicationPct: 20, ChurnCommits: &billingChurn, Role: "library"},
		{Path: "cmd/app", AgentPasses: 0.1, HumanDays: 2.5, Tier: score.TierOnePass, TokensEst: 900, Role: "main"},
		{Path: ".", AgentPasses: 0, HumanDays: 0.3, Tier: score.TierOnePass, TokensEst: 55},
	}
	var buf bytes.Buffer
	if err := WriteRowsTable(&buf, rows); err != nil {
		t.Fatalf("WriteRowsTable error = %v", err)
	}
	// A row whose extractor records no role, or without churn, shows a dash.
	want := strings.Join([]string{
		"PATH              PASSES  DAYS  TIER        FAN_IN  TOKENS  DUP%  CHURN  ROLE",
		"internal/billing     1.2  55.2  FEW_PASSES       7   10000  20.0     42  library",
		"cmd/app              0.1   2.5  ONE_PASS         0     900   0.0      -  main",
		".                    0.0   0.3  ONE_PASS         0      55   0.0      -  -",
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
		Cocomo: score.Cocomo{Model: score.ModelBasic81}, Tiers: score.Tiers{OnePassMax: 1, FewPassesMax: 3},
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
// follow by human days, then path.
func TestSortHotspotDisagreesWithDays(t *testing.T) {
	t.Parallel()

	five, three := 5, 3
	rows := []Row{
		{Path: "idle-b", HumanDays: 2},
		{Path: "wide", AgentPasses: 1, HumanDays: 10, ChurnCommits: &five},
		{Path: "idle-a", HumanDays: 2},
		{Path: "deep", AgentPasses: 4, HumanDays: 8, ChurnCommits: &three},
		{Path: "idle-big", HumanDays: 9},
	}
	if err := SortRows(rows, SortHotspot); err != nil {
		t.Fatal(err)
	}
	want := []string{"deep", "wide", "idle-big", "idle-a", "idle-b"}
	if got := paths(rows); !slices.Equal(got, want) {
		t.Errorf("order = %q, want %q", got, want)
	}
}
