package gate

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/metrics"
)

// gatedMetrics reports the metrics the exemption tests treat as gated.
func gatedMetrics(m string) bool {
	return slices.Contains([]string{"globals", "dup_blocks", "tokens_est", "dup_blocks_cross_pkg"}, m)
}

func TestExemptionValidate(t *testing.T) {
	t.Parallel()

	valid := Exemption{Package: "internal/x", Metric: "globals", Reason: "registry is process-wide by design"}
	with := func(f func(*Exemption)) Exemption {
		e := valid
		f(&e)
		return e
	}
	tests := []struct {
		name    string
		e       Exemption
		wantErr []string
	}{
		{name: "valid", e: valid},
		{name: "root package", e: with(func(e *Exemption) { e.Package = "." })},
		{name: "module row", e: with(func(e *Exemption) { e.Package = metrics.ModuleRowID; e.Metric = "dup_blocks_cross_pkg" })},
		{name: "expires", e: with(func(e *Exemption) { e.Expires = "2026-12-31" })},
		{name: "past expiry still loads", e: with(func(e *Exemption) { e.Expires = "2020-01-01" })},
		{name: "no reason", e: with(func(e *Exemption) { e.Reason = "" }), wantErr: []string{"reason is required"}},
		{name: "blank reason", e: with(func(e *Exemption) { e.Reason = " \t" }), wantErr: []string{"reason is required"}},
		{name: "no package", e: with(func(e *Exemption) { e.Package = "" }), wantErr: []string{"package is required"}},
		{name: "absolute package", e: with(func(e *Exemption) { e.Package = "/src/x" }), wantErr: []string{`package "/src/x"`}},
		{name: "unclean package", e: with(func(e *Exemption) { e.Package = "internal/x/" }), wantErr: []string{`package "internal/x/"`}},
		{name: "dot-slash package", e: with(func(e *Exemption) { e.Package = "./internal/x" }), wantErr: []string{`package "./internal/x"`}},
		{name: "outside module", e: with(func(e *Exemption) { e.Package = "../x" }), wantErr: []string{`package "../x"`}},
		{name: "backslash", e: with(func(e *Exemption) { e.Package = `internal\x` }), wantErr: []string{"package"}},
		{name: "no metric", e: with(func(e *Exemption) { e.Metric = "" }), wantErr: []string{"metric is required"}},
		{name: "ungated metric", e: with(func(e *Exemption) { e.Metric = "sloc" }), wantErr: []string{`metric "sloc": no threshold gates it`}},
		{name: "module-wide on a package", e: with(func(e *Exemption) { e.Metric = "dup_blocks_cross_pkg" }),
			wantErr: []string{"gated on the module row only"}},
		{name: "package metric on the module row", e: with(func(e *Exemption) { e.Package = metrics.ModuleRowID }),
			wantErr: []string{"gated on package rows only"}},
		{name: "bad date", e: with(func(e *Exemption) { e.Expires = "31/12/2026" }), wantErr: []string{`expires "31/12/2026"`}},
		{name: "short date", e: with(func(e *Exemption) { e.Expires = "2026-1-5" }), wantErr: []string{`expires "2026-1-5"`}},
		{name: "every error at once", e: Exemption{}, wantErr: []string{"package is required", "metric is required", "reason is required"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.e.Validate(gatedMetrics)
			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Errorf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want errors containing %q", tt.wantErr)
			}
			for _, w := range tt.wantErr {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("Validate() = %v, want it to contain %q", err, w)
				}
			}
		})
	}
}

func TestExemptionExpired(t *testing.T) {
	t.Parallel()

	day := func(s string) time.Time {
		d, err := time.Parse(time.DateTime, s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	tests := []struct {
		expires string
		now     time.Time
		want    bool
	}{
		{"", day("2099-01-01 00:00:00"), false},
		{"2026-12-31", day("2026-12-30 12:00:00"), false},
		{"2026-12-31", day("2026-12-31 23:59:59"), false}, // applies through the whole day
		{"2026-12-31", day("2027-01-01 00:00:00"), true},
		{"2026-12-31", day("2027-01-01 00:00:00").In(time.FixedZone("west", -5*3600)), false}, // still 12-31 there
		{"not a date", day("2000-01-01 00:00:00"), true},                                      // unreadable never silences
	}
	for _, tt := range tests {
		t.Run(tt.expires+" at "+tt.now.String(), func(t *testing.T) {
			t.Parallel()

			e := Exemption{Package: "x", Metric: "globals", Reason: "r", Expires: tt.expires}
			if got := e.Expired(tt.now); got != tt.want {
				t.Errorf("Expired(%v) = %v, want %v", tt.now, got, tt.want)
			}
		})
	}
}

func TestExemptionMatches(t *testing.T) {
	t.Parallel()

	e := Exemption{Package: "internal/x", Metric: "globals", Reason: "r"}
	tests := []struct {
		pkg, metric string
		want        bool
	}{
		{"internal/x", "globals", true},
		{"internal/x", "dup_blocks", false},
		{"internal/y", "globals", false},
		{"internal/x/sub", "globals", false}, // a directory, not a prefix
		{"internal", "globals", false},
	}
	for _, tt := range tests {
		if got := e.Matches(tt.pkg, &Violation{Metric: tt.metric}); got != tt.want {
			t.Errorf("Matches(%q, %q) = %v, want %v", tt.pkg, tt.metric, got, tt.want)
		}
	}
}

// exemptionResult is a failing result with two violations and a capacity
// warning.
func exemptionResult() Result {
	return Result{
		Violations: []Violation{
			{Metric: "dup_blocks", Base: 1, Head: 2, HasBase: true, Limit: "max_delta +0", Suggestion: "Extract."},
			{Metric: "globals", Head: 1, HasBase: true, Limit: "max_delta +0", Suggestion: "Pass it."},
		},
		Warnings: []Warning{{Metric: "tokens_est", Head: 13000, Limit: "max 16000", Suggestion: "plan a split."}},
	}
}

func TestExemptionsApply(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	globals := Exemption{Package: "internal/x", Metric: "globals", Reason: "process-wide registry"}
	tests := []struct {
		name         string
		pkg          string
		list         []Exemption
		wantMetrics  []string
		wantExempted []string
		wantReason   string
		wantPassed   bool
	}{
		{name: "match by package and metric", pkg: "internal/x", list: []Exemption{globals},
			wantMetrics: []string{"dup_blocks"}, wantExempted: []string{"globals"}, wantReason: "process-wide registry"},
		{name: "other package", pkg: "internal/y", list: []Exemption{globals},
			wantMetrics: []string{"dup_blocks", "globals"}},
		{name: "every violation exempted passes", pkg: "internal/x",
			list:         []Exemption{globals, {Package: "internal/x", Metric: "dup_blocks", Reason: "generated"}},
			wantExempted: []string{"dup_blocks", "globals"}, wantReason: "process-wide registry", wantPassed: true},
		{name: "first reason wins", pkg: "internal/x",
			list:        []Exemption{globals, {Package: "internal/x", Metric: "globals", Reason: "second"}},
			wantMetrics: []string{"dup_blocks"}, wantExempted: []string{"globals"}, wantReason: "process-wide registry"},
		{name: "expired never applies", pkg: "internal/x",
			list:        []Exemption{{Package: "internal/x", Metric: "globals", Reason: "old", Expires: "2026-09-27"}},
			wantMetrics: []string{"dup_blocks", "globals"}},
		{name: "expiring today still applies", pkg: "internal/x",
			list:        []Exemption{{Package: "internal/x", Metric: "globals", Reason: "today", Expires: "2026-09-28"}},
			wantMetrics: []string{"dup_blocks"}, wantExempted: []string{"globals"}, wantReason: "today"},
		{name: "capacity warning never exempted", pkg: "internal/x",
			list:        []Exemption{{Package: "internal/x", Metric: "tokens_est", Reason: "big on purpose"}},
			wantMetrics: []string{"dup_blocks", "globals"}},
		{name: "no exemptions", pkg: "internal/x", wantMetrics: []string{"dup_blocks", "globals"}},
		{name: "blank reason never silences", pkg: "internal/x",
			list:        []Exemption{{Package: "internal/x", Metric: "globals", Reason: " "}},
			wantMetrics: []string{"dup_blocks", "globals"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := exemptionResult()
			NewExemptions(tt.list, now).Apply(tt.pkg, &res)
			var got, exempted []string
			for _, v := range res.Violations {
				got = append(got, v.Metric)
			}
			for _, e := range res.Exempted {
				exempted = append(exempted, e.Metric)
			}
			if !slices.Equal(got, tt.wantMetrics) || !slices.Equal(exempted, tt.wantExempted) {
				t.Errorf("violations %q exempted %q, want %q and %q", got, exempted, tt.wantMetrics, tt.wantExempted)
			}
			if len(tt.wantExempted) > 0 {
				e := res.Exempted[len(res.Exempted)-1]
				if e.Reason != tt.wantReason || e.Limit != "max_delta +0" || e.Suggestion == "" || !e.HasBase {
					t.Errorf("exempted %+v, want the violation's fields and reason %q", e, tt.wantReason)
				}
			}
			if res.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v", res.Passed, tt.wantPassed)
			}
			if len(res.Warnings) != 1 {
				t.Errorf("warnings = %+v, want the capacity warning kept", res.Warnings)
			}
		})
	}
}

func TestExemptionsModuleRow(t *testing.T) {
	t.Parallel()

	x := NewExemptions([]Exemption{
		{Package: metrics.ModuleRowID, Metric: "dup_blocks_cross_pkg", Reason: "vendored twin"},
	}, time.Now())
	pkg := Result{Violations: []Violation{{Metric: "dup_blocks_cross_pkg", Limit: "max_delta +0"}}}
	x.Apply("internal/x", &pkg)
	if len(pkg.Violations) != 1 || pkg.Passed {
		t.Errorf("package row = %+v, want the <module> exemption not to apply to it", pkg)
	}
	mod := Result{Violations: []Violation{{Metric: "dup_blocks_cross_pkg", Limit: "max_delta +0"}}}
	x.Apply(metrics.ModuleRowID, &mod)
	if len(mod.Violations) != 0 || len(mod.Exempted) != 1 || mod.Exempted[0].Reason != "vendored twin" || !mod.Passed {
		t.Errorf("module row = %+v, want its violation exempted", mod)
	}
}

func TestExemptionsExpiredAndStale(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	list := []Exemption{
		{Package: "a", Metric: "globals", Reason: "used"},
		{Package: "b", Metric: "globals", Reason: "stale"},
		{Package: "c", Metric: "globals", Reason: "expired", Expires: "2026-01-01"},
		{Package: "failed", Metric: "globals", Reason: "unknown row"},
		{Package: "a", Metric: "tokens_est", Reason: "only a warning"},
	}
	x := NewExemptions(list, now)
	for _, pkg := range []string{"a", "b", "c"} {
		res := Result{
			Violations: []Violation{{Metric: "globals"}},
			Warnings:   []Warning{{Metric: "tokens_est"}},
		}
		if pkg == "b" {
			res.Violations = nil
		}
		x.Apply(pkg, &res)
	}
	names := func(es []Exemption) []string {
		var out []string
		for _, e := range es {
			out = append(out, e.Reason)
		}
		return out
	}
	if got, want := names(x.Expired()), []string{"expired"}; !slices.Equal(got, want) {
		t.Errorf("Expired() = %q, want %q", got, want)
	}
	unknown := func(e Exemption) bool { return e.Package == "failed" }
	// An exemption on a capacity rule whose finding was only a warning
	// matched nothing, so it is stale too.
	if got, want := names(x.Stale(unknown)), []string{"stale", "only a warning"}; !slices.Equal(got, want) {
		t.Errorf("Stale() = %q, want %q", got, want)
	}

	var none *Exemptions
	res := exemptionResult()
	none.Apply("a", &res)
	if len(res.Violations) != 2 || res.Passed || none.Expired() != nil || none.Stale(unknown) != nil {
		t.Errorf("a nil Exemptions changed %+v or reported exemptions", res)
	}
}
