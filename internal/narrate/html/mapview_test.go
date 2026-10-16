package html

import (
	"bytes"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/narrate"
)

// TestScatterAgentPasses checks that the scatter's y axis is agent_passes,
// not the engineer time: two packages whose passes and human days rank
// them in opposite orders sit where their passes put them, the gridlines
// are whole passes, and the axis and tooltips name the sittings first.
func TestScatterAgentPasses(t *testing.T) {
	t.Parallel()

	pkg := func(path string, passes, days float64, sittings string) narrate.Package {
		return narrate.Package{Path: path, Band: narrate.BandFine, Churn: &narrate.Churn{Commits: 3},
			Rebuild: narrate.Rebuild{AgentPasses: passes, HumanDaysLow: days, HumanDaysHigh: days, SittingsText: sittings}}
	}
	m := &narrate.Module{Name: "M", Packages: []narrate.Package{pkg("hard", 2.5, 1, "3 sittings"), pkg("long", 0.5, 300, "one sitting")}}
	s := build(m, "").Scatter
	if len(s.Points) != 2 {
		t.Fatalf("got %d points, want 2", len(s.Points))
	}
	for _, p := range s.Points {
		passes := m.Packages[p.I].Rebuild.AgentPasses
		want := mapH - marginB - math.Sqrt(passes)/math.Sqrt(2.5*1.05)*(mapH-marginB-marginT)
		if got, err := strconv.ParseFloat(p.Y, 64); err != nil || math.Abs(got-want) > 0.006 {
			t.Errorf("%s y = %s, want %v for %v agent passes", m.Packages[p.I].Path, p.Y, want, passes)
		}
		if first := strings.Split(p.Tip, "\n")[2]; !strings.HasPrefix(first, m.Packages[p.I].Rebuild.SittingsText+" to rebuild · ") ||
			strings.Contains(p.Tip, "days") {
			t.Errorf("%s tooltip = %q, want the sittings first and no days", m.Packages[p.I].Path, p.Tip)
		}
	}
	labels := make([]string, 0, len(s.YTicks))
	for _, tk := range s.YTicks {
		labels = append(labels, tk.Label)
	}
	if want := []string{"0", "1", "2"}; !slices.Equal(labels, want) {
		t.Errorf("y ticks = %q, want %q", labels, want)
	}

	var page bytes.Buffer
	if err := Write(&page, m); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page.String(), ">agent sittings  →</text>") || strings.Contains(page.String(), "days to rebuild") {
		t.Error("the scatter's y axis is not labelled agent sittings")
	}
}
