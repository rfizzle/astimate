package engine

import (
	"errors"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/narrate"
)

func TestReport(t *testing.T) {
	t.Parallel()

	tg := fakeTarget("example.com/m/broken")
	m, failed, err := Report(t.Context(), tg, ReportOptions{})
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	paths := make([]string, 0, len(m.Packages))
	for _, p := range m.Packages {
		paths = append(paths, p.Path)
	}
	if want := []string{".", "big", "x/small"}; !slices.Equal(paths, want) {
		t.Errorf("packages = %q, want %q", paths, want)
	}
	var pe *PackageError
	if len(failed) != 1 || !errors.As(failed[0], &pe) || pe.Path != "broken" {
		t.Errorf("failed = %v, want broken", failed)
	}
	if m.Change != nil || m.Head != "" || m.ModulePath != "example.com/m" || m.Counts.Packages != 3 {
		t.Errorf("module = %+v, want three packages, no head and no change", m)
	}

	t.Run("list failure", func(t *testing.T) {
		t.Parallel()

		tg := fakeTarget("")
		tg.Mod.Root = "/elsewhere"
		if m, _, err := Report(t.Context(), tg, ReportOptions{}); err == nil || m != nil {
			t.Errorf("Report = (%v, %v), want no report and an error", m, err)
		}
	})
}

func TestCeilings(t *testing.T) {
	t.Parallel()

	maxOf := func(v float64) *float64 { return &v }
	ts := []gate.Threshold{
		{Metric: "internal_imports", Kind: gate.Capacity, Max: maxOf(10), Roles: []string{"library"}},
		{Metric: "dup_blocks", Kind: gate.Density, MaxDelta: maxOf(0)},
		{Metric: "sloc", Kind: gate.Capacity, Max: maxOf(1000)},
		{Metric: "sloc", Kind: gate.Capacity, Max: maxOf(2000)},
	}
	want := []narrate.Limit{{Metric: "sloc", Max: 1000}, {Metric: "internal_imports", Max: 10, Roles: []string{"library"}}}
	got := ceilings(&config.Effective{Thresholds: ts})
	if len(got) != len(want) {
		t.Fatalf("ceilings = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Metric != want[i].Metric || got[i].Max != want[i].Max || !slices.Equal(got[i].Roles, want[i].Roles) {
			t.Errorf("ceilings[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	if got := ceilings(&config.Effective{Thresholds: cfg.Thresholds}); len(got) != 5 {
		t.Errorf("default ceilings = %+v, want five", got)
	}
}
