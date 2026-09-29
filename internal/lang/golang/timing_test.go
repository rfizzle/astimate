package golang

import (
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/metrics"
)

// TestMeasureExtractTime times a whole extraction, load included, for the
// notes in calibration/notes: ExtractStdlibAll when ASTIMATE_MEASURE_TIMING
// is "std", otherwise Packages and Extract of every package of the module
// at that directory, with a fresh extractor per run. It logs each run's
// wall time and the median of ASTIMATE_MEASURE_RUNS runs (default 5), and
// the sum of untested_exports. It loads whole modules, so it runs only
// when the variable is set and make check skips it.
func TestMeasureExtractTime(t *testing.T) {
	target := os.Getenv("ASTIMATE_MEASURE_TIMING")
	if target == "" {
		t.Skip("set ASTIMATE_MEASURE_TIMING to std or a module directory")
	}
	runs := 5
	if n, err := strconv.Atoi(os.Getenv("ASTIMATE_MEASURE_RUNS")); err == nil && n > 0 {
		runs = n
	}
	times := make([]time.Duration, 0, runs)
	for i := range runs {
		start := time.Now()
		untested, pkgs := 0, 0
		if target == "std" {
			got, _, err := ExtractStdlibAll(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range got {
				untested += m.UntestedExports
			}
			pkgs = len(got)
		} else {
			e := New()
			paths, err := e.Packages(target)
			if err != nil {
				t.Fatal(err)
			}
			mod := &metrics.ModuleContext{Root: target}
			for _, p := range paths {
				m, err := e.Extract(t.Context(), mod, p)
				if err != nil {
					t.Fatal(err)
				}
				untested += m.UntestedExports
			}
			pkgs = len(paths)
		}
		d := time.Since(start)
		times = append(times, d)
		t.Logf("run %d: %v, %d packages, untested_exports sum %d", i+1, d.Round(time.Millisecond), pkgs, untested)
	}
	slices.Sort(times)
	t.Logf("median of %d runs: %v", runs, times[len(times)/2].Round(time.Millisecond))
}
