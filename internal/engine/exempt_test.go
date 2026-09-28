package engine

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
	"github.com/rfizzle/astimate/internal/report"
)

// exemptNow returns the clock the exemption tests judge expiry at.
func exemptNow() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) }

// exemptTarget returns a target over two fake packages with a module row:
// a adds a global and b is unchanged, both warned for tokens_est, and the
// module row gains a shared block, so a and the module row each have one
// violation. It is gated by globals and dup_blocks_cross_pkg ratchets and a
// tokens_est capacity rule, with exemptions ex, logging to logs.
func exemptTarget(ex []gate.Exemption, logs *bytes.Buffer) *Target {
	const root, modPath = "/mod", "example.com/m"
	pkgs := map[string]metrics.RawMetrics{
		modPath + "/a": {TokensEst: 100, Globals: 1},
		modPath + "/b": {TokensEst: 200},
	}
	zero, capMax := 0.0, 1000.0
	return &Target{
		Mod: &metrics.ModuleContext{Root: root, ModulePath: modPath},
		Ext: &moduleExtractor{Extractor: metricstest.NewFake("go", root, pkgs), cross: 2},
		Cfg: &config.Config{Rebuild: rankParams(), Thresholds: []gate.Threshold{
			{Metric: "dup_blocks_cross_pkg", Kind: gate.Density, MaxDelta: &zero},
			{Metric: "globals", Kind: gate.Density, MaxDelta: &zero, RatchetFromZero: true},
			{Metric: "tokens_est", Kind: gate.Capacity, Max: &capMax, WarnAt: 0.05},
		}, Exemptions: ex},
		Logger: slog.New(slog.NewTextHandler(logs, nil)),
	}
}

// writeExemptBaseline writes exemptTarget's baseline: both packages
// without globals and a module row with one shared block.
func writeExemptBaseline(t *testing.T) string {
	t.Helper()
	one := 1
	pkgs := map[string]metrics.RawMetrics{
		"example.com/m/a":   {TokensEst: 100},
		"example.com/m/b":   {TokensEst: 200},
		metrics.ModuleRowID: {DupBlocksCrossPkg: &one},
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := baseline.Write(path, "", "example.com/m", TokenizerEst, pkgs); err != nil {
		t.Fatal(err)
	}
	return path
}

// rowOf returns c's row for pkg, failing the test when there is none.
func rowOf(t *testing.T, c *report.Check, pkg string) *report.Report {
	t.Helper()
	if r := exemptionRow(c, pkg); r != nil {
		return r
	}
	t.Fatalf("check has no row %s", pkg)
	return nil
}

// limits returns the limits of fs, so notices are told apart from
// capacity warnings.
func limits(fs []report.Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Metric+" "+f.Limit)
	}
	return out
}

func TestCheckExemptions(t *testing.T) {
	t.Parallel()

	registry := gate.Exemption{Package: "a", Metric: "globals", Reason: "process-wide registry"}
	clients := gate.Exemption{Package: metrics.ModuleRowID, Metric: "dup_blocks_cross_pkg", Reason: "generated clients"}
	unused := gate.Exemption{Package: "b", Metric: "globals", Reason: "b needed it once"}
	gone := gate.Exemption{Package: "gone", Metric: "globals", Reason: "package since deleted"}
	warnOnly := gate.Exemption{Package: "a", Metric: "tokens_est", Reason: "big on purpose"}
	expired := gate.Exemption{Package: "a", Metric: "globals", Reason: "old", Expires: "2026-09-27"}
	all := []gate.Exemption{registry, clients, unused, gone, warnOnly, expired}

	t.Run("all", func(t *testing.T) {
		t.Parallel()

		var logs bytes.Buffer
		c, failed, err := Check(t.Context(), exemptTarget(all, &logs), CheckOptions{
			BaselineFile: writeExemptBaseline(t), All: true, Now: exemptNow(),
		})
		if err != nil || len(failed) != 0 {
			t.Fatalf("Check = (%v, %v), want no error", failed, err)
		}
		if c.Failed() {
			t.Error("check failed although every violation is exempted")
		}
		a := rowOf(t, c, "a")
		if len(a.Violations) != 0 || len(a.Exemptions) != 1 || a.Exemptions[0].Reason != registry.Reason ||
			a.Exemptions[0].Metric != "globals" || a.Passed == nil || !*a.Passed {
			t.Errorf("a = violations %+v exempted %+v passed %v, want globals exempted with its reason", a.Violations, a.Exemptions, a.Passed)
		}
		// The capacity warning stays a warning; the exemption on it and the
		// expired one are reported as notices.
		wantA := []string{"tokens_est max 1000", "globals exemption expired 2026-09-27", "tokens_est stale exemption"}
		if got := limits(a.Warnings); !slices.Equal(got, wantA) {
			t.Errorf("a warnings = %q, want %q", got, wantA)
		}
		for _, w := range a.Warnings[1:] {
			if !strings.Contains(w.Suggestion, "Its reason: ") {
				t.Errorf("notice %q does not carry the exemption's reason", w.Suggestion)
			}
		}
		if got, want := limits(rowOf(t, c, "b").Warnings), []string{"tokens_est max 1000", "globals stale exemption"}; !slices.Equal(got, want) {
			t.Errorf("b warnings = %q, want %q", got, want)
		}
		mod := rowOf(t, c, metrics.ModuleRowID)
		if len(mod.Violations) != 0 || len(mod.Exemptions) != 1 || mod.Exemptions[0].Reason != clients.Reason {
			t.Errorf("module row = violations %+v exempted %+v, want the cross-package copy exempted", mod.Violations, mod.Exemptions)
		}
		// An exemption for a package the module does not have is stale on
		// the module row.
		if got, want := limits(mod.Warnings), []string{"globals stale exemption"}; !slices.Equal(got, want) ||
			!strings.Contains(mod.Warnings[0].Suggestion, "on gone matched no violation: the module has no such package") {
			t.Errorf("module row warnings = %+v, want the stale exemption for gone", mod.Warnings)
		}
		if strings.Contains(logs.String(), "exemption") {
			t.Errorf("every notice had a row, but some were logged:\n%s", logs.String())
		}
	})
	// A scoped check never calls an exemption stale: the rows it did not
	// select were never evaluated. An expired exemption is still reported,
	// on its row when the check has it and in the log otherwise.
	for _, opts := range []CheckOptions{
		{Packages: []string{"example.com/m/a"}},
		{All: false}, // a baseline file without a ref checks every package, but not as --all
		{All: true, Packages: []string{"example.com/m/a"}},
	} {
		t.Run("scoped", func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer
			expiredB := gate.Exemption{Package: "b", Metric: "globals", Reason: "old b", Expires: "2026-01-01"}
			opts.BaselineFile, opts.Now = writeExemptBaseline(t), exemptNow()
			c, failed, err := Check(t.Context(), exemptTarget(append(slices.Clone(all), expiredB), &logs), opts)
			if err != nil || len(failed) != 0 {
				t.Fatalf("Check = (%v, %v), want no error", failed, err)
			}
			for _, r := range append([]*report.Report{&c.Module.Report}, rowPtrs(c)...) {
				for _, w := range r.Warnings {
					if w.Limit == "stale exemption" {
						t.Errorf("%s: stale notice %q in a scoped check", r.PackagePath, w.Suggestion)
					}
				}
			}
			a := rowOf(t, c, "a")
			if got := limits(a.Warnings); !slices.Contains(got, "globals exemption expired 2026-09-27") {
				t.Errorf("a warnings = %q, want the expired notice", got)
			}
			if len(a.Exemptions) != 1 {
				t.Errorf("a exempted = %+v, want globals", a.Exemptions)
			}
			inCheck := exemptionRow(c, "b") != nil
			if logged := strings.Contains(logs.String(), "exemption expired; ignored") && strings.Contains(logs.String(), "old b"); logged == inCheck {
				t.Errorf("b in check = %v, expired exemption logged = %v; want it logged only when b has no row:\n%s", inCheck, logged, logs.String())
			}
		})
	}
	t.Run("expired exemption does not silence", func(t *testing.T) {
		t.Parallel()

		var logs bytes.Buffer
		c, _, err := Check(t.Context(), exemptTarget([]gate.Exemption{expired, clients}, &logs), CheckOptions{
			BaselineFile: writeExemptBaseline(t), All: true, Now: exemptNow(),
		})
		if err != nil {
			t.Fatal(err)
		}
		a := rowOf(t, c, "a")
		if !c.Failed() || len(a.Violations) != 1 || len(a.Exemptions) != 0 || a.Passed == nil || *a.Passed {
			t.Errorf("a = violations %+v exempted %+v passed %v, want the globals violation to fail", a.Violations, a.Exemptions, a.Passed)
		}
		// The day before it expired, it applied.
		c, _, err = Check(t.Context(), exemptTarget([]gate.Exemption{expired, clients}, &logs), CheckOptions{
			BaselineFile: writeExemptBaseline(t), All: true, Now: exemptNow().AddDate(0, 0, -1),
		})
		if err != nil {
			t.Fatal(err)
		}
		if c.Failed() || len(rowOf(t, c, "a").Exemptions) != 1 {
			t.Errorf("a day earlier the check failed = %v, want the exemption to apply", c.Failed())
		}
	})
	t.Run("unjudged rows are not stale", func(t *testing.T) {
		t.Parallel()

		var logs bytes.Buffer
		tg := exemptTarget([]gate.Exemption{clients, {Package: "a", Metric: "globals", Reason: "r"}}, &logs)
		// Without the globals rule for this language the exemption on it
		// cannot be judged; one config may serve another language's module.
		tg.Cfg.Thresholds = slices.DeleteFunc(slices.Clone(tg.Cfg.Thresholds), func(r gate.Threshold) bool { return r.Metric == "globals" })
		// A baseline file without the module row skips the module-wide
		// rules, so the <module> exemption cannot be judged either.
		path := filepath.Join(t.TempDir(), "baseline.json")
		if err := baseline.Write(path, "", "example.com/m", TokenizerEst, map[string]metrics.RawMetrics{
			"example.com/m/a": {TokensEst: 100}, "example.com/m/b": {TokensEst: 200},
		}); err != nil {
			t.Fatal(err)
		}
		c, _, err := Check(t.Context(), tg, CheckOptions{BaselineFile: path, All: true, Now: exemptNow()})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range append([]*report.Report{&c.Module.Report}, rowPtrs(c)...) {
			for _, w := range r.Warnings {
				if w.Limit == "stale exemption" {
					t.Errorf("%s: stale notice %q for an exemption the check could not judge", r.PackagePath, w.Suggestion)
				}
			}
		}
	})
}

// rowPtrs returns pointers to c's package reports.
func rowPtrs(c *report.Check) []*report.Report {
	out := make([]*report.Report, 0, len(c.Packages))
	for i := range c.Packages {
		out = append(out, &c.Packages[i].Report)
	}
	return out
}
