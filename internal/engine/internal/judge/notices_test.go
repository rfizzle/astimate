package judge

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
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
	list := []gate.Exemption{
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
			jc := New(Options{Config: config.Effective{Thresholds: rules, Exemptions: list}, Now: now, Logger: logger})
			jc.Notices(c, Scope{All: tt.all, Failed: []string{"broken"}})
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
