package report

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/score"
)

// rankRows are rows with ties on every sort key, in no particular order.
func rankRows() []Row {
	return []Row{
		{Path: "c", AgentPasses: 0.5, HumanDays: 2, FanIn: 1, TokensEst: 100, DuplicationPct: 0},
		{Path: "a", AgentPasses: 0.5, HumanDays: 1, FanIn: 3, TokensEst: 300, DuplicationPct: 10},
		{Path: "d", AgentPasses: 1.5, HumanDays: 1, FanIn: 1, TokensEst: 100, DuplicationPct: 10},
		{Path: "b", AgentPasses: 0.1, HumanDays: 2, FanIn: 0, TokensEst: 200, DuplicationPct: 30},
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

	rows := []Row{
		{Path: "internal/billing", AgentPasses: 1.2, HumanDays: 55.2, Tier: score.TierFewPasses,
			FanIn: 7, TokensEst: 10000, DuplicationPct: 20},
		{Path: ".", AgentPasses: 0, HumanDays: 0.3, Tier: score.TierOnePass, TokensEst: 55},
	}
	var buf bytes.Buffer
	if err := WriteRowsTable(&buf, rows); err != nil {
		t.Fatalf("WriteRowsTable error = %v", err)
	}
	want := strings.Join([]string{
		"PATH              PASSES  DAYS  TIER        FAN_IN  TOKENS  DUP%",
		"internal/billing     1.2  55.2  FEW_PASSES       7   10000  20.0",
		".                    0.0   0.3  ONE_PASS         0      55   0.0",
		"",
	}, "\n")
	if got := buf.String(); got != want {
		t.Errorf("WriteRowsTable =\n%s\nwant:\n%s", got, want)
	}
}
