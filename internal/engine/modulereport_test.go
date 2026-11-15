package engine

import (
	"errors"
	"slices"
	"testing"
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
