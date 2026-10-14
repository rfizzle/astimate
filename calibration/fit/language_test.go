package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/fit/internal/emit"
	"github.com/rfizzle/astimate/calibration/fit/internal/pool"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
)

// takeFloors clears the floor of every rule of ts and returns the floors
// by metric. No fit produces a floor: the default's are set by hand
// (SPEC.md 8.2), so the tests compare them apart from the fitted rules.
func takeFloors(ts []gate.Threshold) map[string]float64 {
	floors := map[string]float64{}
	for i := range ts {
		if f := ts[i].Floor; f != nil {
			floors[ts[i].Metric] = *f
			ts[i].Floor = nil
		}
	}
	return floors
}

// takeOverMax clears every over_max_delta of ts and returns them by metric.
func takeOverMax(ts []gate.Threshold) map[string]float64 {
	out := map[string]float64{}
	for i := range ts {
		if d := ts[i].OverMaxDelta; d != nil {
			out[ts[i].Metric] = *d
			ts[i].OverMaxDelta = nil
		}
	}
	return out
}

// takeSeverities clears every rule's severity in ts and returns the
// metrics of the warn rules, sorted, since no fit produces a severity: the
// gate validation sets them (SPEC.md 8.2 and 11.4).
func takeSeverities(ts []gate.Threshold) []string {
	var out []string
	for i := range ts {
		if ts[i].Warns() {
			out = append(out, ts[i].Metric)
		}
		ts[i].Severity = ""
	}
	slices.Sort(out)
	return out
}

// wantWarns is the metrics of the default's warn rules, sorted, at the top
// level and in the typescript override alike (SPEC.md 8.2).
func wantWarns() []string {
	return []string{"cognitive_p90", "globals", "internal_imports", "untested_exports"}
}

// takeRoles returns ts without the rules that judge main packages only,
// which no fit produces (the default's has_tests past 300 SLOC for main,
// SPEC.md 8.2), and with every other rule's roles cleared, and the roles
// it cleared, space-joined by metric.
func takeRoles(ts []gate.Threshold) ([]gate.Threshold, map[string]string) {
	out := make([]gate.Threshold, 0, len(ts))
	roles := map[string]string{}
	for _, t := range ts {
		if slices.Equal(t.Roles, []string{"main"}) {
			continue
		}
		if len(t.Roles) > 0 {
			roles[t.Metric] = strings.Join(t.Roles, " ")
			t.Roles = nil
		}
		out = append(out, t)
	}
	return out, roles
}

// wantRoles returns the roles the default's rules carry (SPEC.md 8.2),
// the same at the top level and in the typescript override, as takeRoles
// returns them.
func wantRoles() map[string]string {
	notMain := "library test-support generated-only"
	return map[string]string{"untested_exports": "library generated-only", "exported_symbols": notMain,
		"internal_imports": notMain, "has_tests": notMain}
}

// wantFloors returns the default's density floors (SPEC.md 8.2), the same
// at the top level and in the typescript override.
func wantFloors() map[string]float64 {
	return map[string]float64{"duplication_pct": 10, "max_nesting": 4, "cognitive_p90": 10}
}

// synthRows returns n rows with sloc, largest_file_sloc and
// duplication_pct running 1..n and has_tests set.
func synthRows(n int) []pool.Row {
	rows := make([]pool.Row, n)
	for i := range rows {
		v := i + 1
		rows[i].Module = "example.com/m"
		rows[i].Metrics.SLOC = v
		rows[i].Metrics.LargestFileSLOC = v
		rows[i].Metrics.DuplicationPct = float64(v)
		rows[i].Metrics.HasTests = true
	}
	return rows
}

// tsDataPath is the committed TypeScript corpus data the shipped
// typescript override is fitted from.
const tsDataPath = "../data/2026-09-28-typescript/packages.jsonl"

// tsCommittedOverride is the override block fitted from tsDataPath, which
// the embedded default carries under languages.typescript.
const tsCommittedOverride = "../thresholds/astimate-thresholds-2026-09-28-typescript.yaml"

// tsCommittedReport is the report of that fit.
const tsCommittedReport = "../reports/thresholds-2026-09-28-typescript.md"

// tsOptions are the flags the committed override was fitted with, from
// the repository root, writing into dir.
func tsOptions(dir string) options {
	return options{
		data:     "calibration/data/2026-09-28-typescript/packages.jsonl",
		baseData: "calibration/data/2026-09-28-corpus/packages.jsonl",
		language: "typescript", date: "2026-09-28", suffix: suffixAuto,
		out: filepath.Join(dir, "override.yaml"), report: filepath.Join(dir, "report.md"),
	}
}

// writeRows writes rows as a packages.jsonl file in dir and returns its
// path.
func writeRows(t *testing.T, dir string, rows []pool.Row) string {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for i := range rows {
		if err := enc.Encode(&rows[i]); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "packages.jsonl")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestFitLanguage fits synthetic TypeScript rows into a languages override
// and checks the block: it keeps the base's config_version, suffixed in
// reports; it replaces exactly the rules with a fitted statistic, with the
// fitted limits and the base rules' shape; and it leaves the zero-tolerance
// ratchets, the requirement and the metric no row measures to the top
// level.
func TestFitLanguage(t *testing.T) {
	dir := t.TempDir()
	rows := synthRows(100)
	for i := range rows {
		rows[i].Language = "typescript"
	}
	res, err := fit(options{data: writeRows(t, dir, rows), language: "typescript", date: "2026-09-28", suffix: suffixAuto,
		out: filepath.Join(dir, "o.yaml"), report: filepath.Join(dir, "r.md")})
	if err != nil {
		t.Fatalf("fit: %v", err)
	}
	def, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	if want := def.Version + "+typescript"; res.version != want {
		t.Errorf("version = %q, want %q", res.version, want)
	}
	block, err := os.ReadFile(res.out)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(block, []byte("config_version")) || bytes.Contains(block, []byte("rebuild:")) {
		t.Errorf("override block sets config_version or rebuild:\n%s", block)
	}
	merged, err := emit.MergeLanguage(config.Default(), "typescript", block)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse(merged)
	if err != nil {
		t.Fatalf("merged override does not validate: %v", err)
	}
	ts := cfg.ForLanguage("typescript")
	if ts.Version != res.version || ts.Rebuild != cfg.Rebuild {
		t.Errorf("typescript = version %q rebuild %+v, want %q and the top level's", ts.Version, ts.Rebuild, res.version)
	}
	var replaced []string
	for i, r := range ts.Thresholds {
		top := cfg.Thresholds[i]
		if r.Metric != top.Metric || r.Kind != top.Kind || r.WarnAt != top.WarnAt || r.RatchetFromZero != top.RatchetFromZero {
			t.Errorf("thresholds[%d] %s: shape differs from the top-level %s", i, r.Metric, top.Metric)
		}
		if !reflect.DeepEqual(r, top) {
			replaced = append(replaced, r.Metric)
		}
	}
	// Every metric the synthetic rows vary, and the ones they leave at 0
	// that still carry a fitted max, are replaced wherever the fit moved
	// them from the default; sloc, largest_file_sloc and duplication_pct
	// move.
	for _, m := range []string{"sloc", "largest_file_sloc", "duplication_pct"} {
		if !slices.Contains(replaced, m) {
			t.Errorf("%s not replaced; replaced %q", m, replaced)
		}
	}
	for _, m := range []string{"dup_blocks", "untested_exports", "globals", "init_funcs", "has_tests",
		"dup_blocks_cross_pkg", "changed_func_cognitive_max"} {
		if bytes.Contains(block, []byte("metric: "+m+"\n")) {
			t.Errorf("override names %s, which it must leave to the top level", m)
		}
	}
	if got := *ruleOn(t, ts.Thresholds, "sloc").Max; got != 90 {
		t.Errorf("typescript sloc max = %v, want the p90 90", got)
	}
	report, err := os.ReadFile(res.report)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Threshold override " + res.version, "## Override",
		// The synthetic rows count no functions either.
		"Inherited, measured by no typescript row: `changed_func_cognitive_max`, `dup_blocks_cross_pkg`."} {
		if !strings.Contains(string(report), want) {
			t.Errorf("report does not contain %q", want)
		}
	}
}

// ruleOn returns the rule on metric, failing the test when there is none.
func ruleOn(t *testing.T, rules []gate.Threshold, metric string) gate.Threshold {
	t.Helper()
	for _, r := range rules {
		if r.Metric == metric {
			return r
		}
	}
	t.Fatalf("no rule on %s", metric)
	return gate.Threshold{}
}

// TestFitLanguageRejectsMixedRows checks that a fit pools one language:
// Go rows under --language typescript, and TypeScript rows without it, are
// errors.
func TestFitLanguageRejectsMixedRows(t *testing.T) {
	dir := t.TempDir()
	goRows := synthRows(10)
	tsRows := synthRows(10)
	for i := range tsRows {
		tsRows[i].Language = "typescript"
	}
	tests := []struct {
		name, lang, wantErr string
		rows                []pool.Row
	}{
		{"go rows as typescript", "typescript", "row 1 is go, not typescript", goRows},
		{"typescript rows as go", "", "fit it with --language typescript", tsRows},
		{"mixed", "typescript", "row 11 is go, not typescript", append(slices.Clone(tsRows), goRows...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := t.TempDir()
			_, err := fit(options{data: writeRows(t, sub, tt.rows), language: tt.lang, date: "2026-09-28", suffix: suffixAuto,
				out: filepath.Join(dir, "o.yaml"), report: filepath.Join(dir, "r.md")})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("fit error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestCommittedTypeScriptOverride refits the committed TypeScript data and
// checks it reproduces the committed override block and report byte for
// byte, apart from the output paths the report names.
func TestCommittedTypeScriptOverride(t *testing.T) {
	want := make(map[string][]byte, 2)
	for _, p := range []string{tsCommittedOverride, tsCommittedReport} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		want[p] = data
	}
	dir := t.TempDir()
	// The committed files name the data relative to the repository root.
	t.Chdir("../..")
	opts := tsOptions(dir)
	// The override was fitted against the thresholds candidate, before the
	// rebuild fit gave the default a rebuild- version; pin that base.
	opts.base = filepath.Join("calibration", "thresholds", filepath.Base(committedCandidate))
	res, err := fit(opts)
	if err != nil {
		t.Fatalf("fit: %v", err)
	}
	if res.version != shippedVersion+"+typescript" {
		t.Errorf("version = %q, want %q", res.version, shippedVersion+"+typescript")
	}
	for _, f := range []struct{ got, want string }{{res.out, tsCommittedOverride}, {res.report, tsCommittedReport}} {
		got, err := os.ReadFile(f.got)
		if err != nil {
			t.Fatal(err)
		}
		got = bytes.ReplaceAll(got, []byte(filepath.ToSlash(opts.out)), []byte("calibration/thresholds/astimate-thresholds-2026-09-28-typescript.yaml"))
		if !bytes.Equal(got, want[f.want]) {
			t.Errorf("refit of %s differs from the committed %s; rerun the fit command in calibration/corpus.md", tsDataPath, f.want)
		}
	}
}

// TestDefaultCarriesTypeScriptOverride checks the embedded default's
// languages.typescript section is exactly the committed override block: the
// same rules, resolved against the top level. The floors and the
// over_max_delta allowances, which no fit produces, are compared apart.
func TestDefaultCarriesTypeScriptOverride(t *testing.T) {
	block, err := os.ReadFile(tsCommittedOverride)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := emit.MergeLanguage(config.Default(), "typescript", block)
	if err != nil {
		t.Fatal(err)
	}
	want, err := config.Parse(merged)
	if err != nil {
		t.Fatal(err)
	}
	def, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	got, exp := def.ForLanguage("typescript"), want.ForLanguage("typescript")
	if floors := takeFloors(got.Thresholds); !reflect.DeepEqual(floors, wantFloors()) {
		t.Errorf("default typescript floors = %v, want %v", floors, wantFloors())
	}
	takeFloors(exp.Thresholds)
	// The allowances are set by hand from the validation fit or scaled from
	// the top level's (SPEC.md 8.2), not produced by the override fit.
	wantOver := map[string]float64{"changed_func_cognitive_max": 5, "tokens_est": 4400, "largest_file_sloc": 75, "exported_symbols": 5, "internal_imports": 1, "sloc": 375}
	if over := takeOverMax(got.Thresholds); !reflect.DeepEqual(over, wantOver) {
		t.Errorf("default typescript over_max_delta = %v, want %v", over, wantOver)
	}
	takeOverMax(exp.Thresholds)
	// So are the severities.
	if warns := takeSeverities(got.Thresholds); !slices.Equal(warns, wantWarns()) {
		t.Errorf("default typescript warn rules = %v, want %v", warns, wantWarns())
	}
	takeSeverities(exp.Thresholds)
	// The roles are set by hand too (SPEC.md 8.2).
	var roles map[string]string
	if got.Thresholds, roles = takeRoles(got.Thresholds); !reflect.DeepEqual(roles, wantRoles()) {
		t.Errorf("default typescript roles = %v, want %v", roles, wantRoles())
	}
	exp.Thresholds, _ = takeRoles(exp.Thresholds)
	if got.Version != exp.Version || got.Rebuild != exp.Rebuild || !reflect.DeepEqual(got.Thresholds, exp.Thresholds) {
		t.Errorf("default typescript override differs from %s:\n%+v\n%+v", tsCommittedOverride, got, exp)
	}
}
