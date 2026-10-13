package judge

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// skipper is an extractor whose Skipped returns list or err; its other
// methods are never called.
type skipper struct {
	metrics.Extractor
	list []metrics.SkippedPackage
	err  error
}

// Skipped returns s.list and s.err.
func (s skipper) Skipped(string) ([]metrics.SkippedPackage, error) { return s.list, s.err }

func TestSkips(t *testing.T) {
	t.Parallel()

	const root = "/work/mod"
	ext := skipper{list: []metrics.SkippedPackage{
		{Package: "example.com/m/b", Reason: "missing module", Error: "loading example.com/m/b: " + root + "/b/b.go:3:8: no module"},
		{Package: "example.com/m/d", Reason: "type error in test files", Error: "loading example.com/m/d: d_test.go:1: bad"},
	}}
	s, err := SkippedIn(ext, root)
	if err != nil {
		t.Fatal(err)
	}

	head := []string{"example.com/m/a", "example.com/m/c"}
	want := []string{"example.com/m/a", "example.com/m/b", "example.com/m/c", "example.com/m/d"}
	if got := s.With(head); !slices.Equal(got, want) {
		t.Errorf("With = %v, want %v", got, want)
	}

	if err := s.Failure("example.com/m/a"); err != nil {
		t.Errorf("Failure of a loaded package = %v, want nil", err)
	}
	err = s.Failure("example.com/m/b")
	if !errors.Is(err, ErrSkipped) || !strings.Contains(err.Error(), "(missing module)") ||
		!strings.Contains(err.Error(), "b/b.go:3:8") || strings.Contains(err.Error(), root) {
		t.Errorf("Failure = %v, want ErrSkipped with the reason and a module-relative error", err)
	}

	got := s.Report("example.com/m", []string{"example.com/m/d"})
	if len(got) != 2 || got[0].Package != "b" || got[0].Selected || got[1].Package != "d" || !got[1].Selected {
		t.Errorf("Report = %+v, want b unselected and d selected", got)
	}
}

func TestSkipsWithoutSkipper(t *testing.T) {
	t.Parallel()

	var ext metrics.Extractor
	s, err := SkippedIn(ext, "/work/mod")
	if err != nil {
		t.Fatal(err)
	}
	head := []string{"a"}
	if got := s.With(head); !slices.Equal(got, head) || s.Report("m", head) != nil || s.Failure("a") != nil {
		t.Errorf("an extractor without Skipper skipped something: %v", got)
	}

	failing := skipper{err: errors.New("load failed")}
	if _, err := SkippedIn(failing, "/work/mod"); err == nil {
		t.Error("SkippedIn hid the extractor's error")
	}
}
