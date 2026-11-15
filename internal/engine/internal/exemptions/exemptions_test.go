package exemptions

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/gate/exempt"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

// TestNotices checks where each kind of notice lands: an expired exemption
// on its row, or in a log without one; a stale one on its row, on the
// module row for a package the module lacks, and nowhere when its row
// could not be judged or the check did not judge every row.
func TestNotices(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	list := []exempt.Entry{
		{Package: "a", Metric: "globals", Reason: "old", Expires: "2026-01-01"},
		{Package: "gone", Metric: "globals", Reason: "elsewhere", Expires: "2026-01-01"},
		{Package: "a", Metric: "dup_blocks", Reason: "stale here"},
		{Package: "nowhere", Metric: "dup_blocks", Reason: "no such package"},
		{Package: "broken", Metric: "dup_blocks", Reason: "failed to extract"},
		{Package: "a", Metric: "sloc", Reason: "no rule on it"},
		{Package: metrics.ModuleRowID, Metric: "dup_blocks_cross_pkg", Reason: "module not judged"},
	}
	rules := []gate.Threshold{{Metric: "globals"}, {Metric: "dup_blocks"}, {Metric: "dup_blocks_cross_pkg"}}
	for _, tt := range []struct {
		name       string
		all        bool
		wantA      []string
		wantModule []string
		wantLogs   []string
	}{
		{
			name:     "changed packages only",
			wantA:    []string{"exemption expired 2026-01-01"},
			wantLogs: []string{"exemption expired; ignored"},
		},
		{
			name:       "every package",
			all:        true,
			wantA:      []string{"exemption expired 2026-01-01", "stale exemption"},
			wantModule: []string{"stale exemption"},
			wantLogs:   []string{"exemption expired; ignored"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &report.Check{
				Packages: []report.CheckedPackage{{Report: report.Report{PackagePath: "a"}}},
				Module:   &report.CheckedPackage{Report: report.Report{PackagePath: metrics.ModuleRowID}},
			}
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			New(config.Effective{Thresholds: rules, Exemptions: list}, now, logger).Notices(c, Scope{All: tt.all, Failed: []string{"broken"}})
			if got := limits(c.Packages[0].Report.Warnings); !equal(got, tt.wantA) {
				t.Errorf("row a warnings = %q, want %q", got, tt.wantA)
			}
			if got := limits(c.Module.Report.Warnings); !equal(got, tt.wantModule) {
				t.Errorf("module row warnings = %q, want %q", got, tt.wantModule)
			}
			for _, want := range tt.wantLogs {
				if !strings.Contains(logs.String(), want) {
					t.Errorf("logs lack %q:\n%s", want, logs.String())
				}
			}
			if tt.all && !strings.Contains(c.Module.Report.Warnings[0].Suggestion, "the module has no such package") {
				t.Errorf("module row notice = %q, want it to say the package is missing", c.Module.Report.Warnings[0].Suggestion)
			}
		})
	}
}

// TestSetApply checks that a Set silences the violations its unexpired
// exemptions match on their row only, and ignores an expired one.
func TestSetApply(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	list := []exempt.Entry{
		{Package: "a", Metric: "globals", Reason: "kept"},
		{Package: "a", Metric: "dup_blocks", Reason: "old", Expires: "2026-01-01"},
	}
	ex := New(config.Effective{Exemptions: list}, now, slog.New(slog.DiscardHandler))
	for _, tt := range []struct {
		pkg          string
		wantLeft     []string
		wantExempted []string
	}{
		{pkg: "a", wantLeft: []string{"dup_blocks"}, wantExempted: []string{"globals"}},
		{pkg: "b", wantLeft: []string{"globals", "dup_blocks"}},
	} {
		t.Run(tt.pkg, func(t *testing.T) {
			t.Parallel()
			res := gate.Result{Violations: []gate.Violation{{Metric: "globals"}, {Metric: "dup_blocks"}}}
			ex.Apply(tt.pkg, &res)
			left := make([]string, 0, len(res.Violations))
			for _, v := range res.Violations {
				left = append(left, v.Metric)
			}
			exempted := make([]string, 0, len(res.Exempted))
			for _, e := range res.Exempted {
				exempted = append(exempted, e.Metric)
			}
			if !equal(left, tt.wantLeft) || !equal(exempted, tt.wantExempted) {
				t.Errorf("Apply(%s) left %q and exempted %q, want %q and %q", tt.pkg, left, exempted, tt.wantLeft, tt.wantExempted)
			}
		})
	}
}

func limits(fs []report.Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Limit)
	}
	return out
}

func equal(a, b []string) bool {
	return strings.Join(a, "|") == strings.Join(b, "|")
}

// TestNoticesNameTheirFile checks that a notice about an exemption read
// from the exemptions file names that file, on a row and in a log, and
// that one about the configuration's own exemption reads as it always
// has.
func TestNoticesNameTheirFile(t *testing.T) {
	t.Parallel()

	const file = config.ExemptionsFileName
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	list := []exempt.Entry{
		{Package: "a", Metric: "globals", Reason: "old", Expires: "2026-01-01", Source: file},
		{Package: "a", Metric: "dup_blocks", Reason: "unused", Source: file},
		{Package: "b", Metric: "dup_blocks", Reason: "own", Expires: "2026-01-01"},
		{Package: "b", Metric: "globals", Reason: "own unused"},
		{Package: "gone", Metric: "dup_blocks", Reason: "deleted", Source: file},
		{Package: "nowhere", Metric: "globals", Reason: "not checked", Expires: "2026-01-01", Source: file},
	}
	rules := []gate.Threshold{{Metric: "globals"}, {Metric: "dup_blocks"}}
	c := &report.Check{
		Packages: []report.CheckedPackage{
			{Report: report.Report{PackagePath: "a"}},
			{Report: report.Report{PackagePath: "b"}},
		},
		Module: &report.CheckedPackage{Report: report.Report{PackagePath: metrics.ModuleRowID}},
	}
	var logs bytes.Buffer
	ex := New(config.Effective{Thresholds: rules, Exemptions: list}, now, slog.New(slog.NewTextHandler(&logs, nil)))
	ex.Notices(c, Scope{All: true})

	suggestions := func(r *report.Report) []string {
		out := make([]string, 0, len(r.Warnings))
		for _, w := range r.Warnings {
			out = append(out, w.Suggestion)
		}
		return out
	}
	for _, tt := range []struct {
		row  *report.Report
		want []string
	}{
		{&c.Packages[0].Report, []string{
			"The exemption for globals on a in " + file + " expired on 2026-01-01 and no longer applies; " +
				"fix the finding or renew the exemption with a new date. Its reason: old",
			"The exemption for dup_blocks on a in " + file + " matched no violation; remove it from " + file + ". Its reason: unused",
		}},
		{&c.Packages[1].Report, []string{
			"The exemption for dup_blocks on b expired on 2026-01-01 and no longer applies; " +
				"fix the finding or renew the exemption with a new date. Its reason: own",
			"The exemption for globals on b matched no violation; remove it from the config. Its reason: own unused",
		}},
		{&c.Module.Report, []string{
			"The exemption for dup_blocks on gone in " + file + " matched no violation: the module has no such package. " +
				"Remove it from " + file + ". Its reason: deleted",
		}},
	} {
		if got := suggestions(tt.row); !equal(got, tt.want) {
			t.Errorf("%s notices =\n%q\nwant\n%q", tt.row.PackagePath, got, tt.want)
		}
	}
	if want := "package=nowhere metric=globals expires=2026-01-01 reason=\"not checked\" file=" + file; !strings.Contains(logs.String(), want) {
		t.Errorf("logs lack %q:\n%s", want, logs.String())
	}
}
