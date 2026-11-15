package blocklog

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate/exempt"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

const testModule = "example.com/m"

// testNow is the time every test line records.
func testNow() time.Time { return time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC) }

// testOptions returns options for a module rooted at root whose
// configuration is the embedded default with the block log at logPath
// (off when empty) and the given exemptions.
func testOptions(t *testing.T, root, logPath string, exemptions ...exempt.Entry) *Options {
	t.Helper()
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Hook.BlockLog = logPath
	cfg.Exemptions = exemptions
	return &Options{Root: root, Module: testModule, Config: cfg, Version: "v0.test", Now: testNow}
}

func ptr(v float64) *float64 { return &v }

// row returns a checked package at path with exported_symbols and sloc as
// given, its baseline present unless noBase.
func row(path string, exports, sloc int, noBase bool) report.CheckedPackage {
	r := report.Report{Language: "go", PackagePath: path, ModulePath: testModule, ConfigVersion: "cfg-1",
		Metrics: metrics.RawMetrics{ExportedSymbols: exports, SLOC: sloc}}
	if !noBase {
		r.Baseline = &report.Baseline{}
	}
	return report.CheckedPackage{Report: r}
}

// blocked returns a check whose package a breaches exported_symbols, with
// sloc merged into nothing, and whose package b breaches sloc with
// tokens_est merged into it.
func blocked() *report.Check {
	a := row("a", 63, 900, false)
	a.Report.Violations = []report.Finding{{Metric: "exported_symbols", Base: ptr(58), Head: 63, Limit: "max 60"}}
	b := row("b", 10, 1200, true)
	b.Report.Violations = []report.Finding{{Metric: "sloc", Head: 1200, Limit: "max 1000; tokens_est 17000, max 16000",
		Also: []string{"tokens_est"}}}
	return &report.Check{Packages: []report.CheckedPackage{a, b}}
}

// readLog returns the lines of the log at path, failing on a malformed
// one; nil when the file does not exist.
func readLog(t *testing.T, path string) []Line {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	lines, malformed, err := Read(bytes.NewReader(data))
	if err != nil || malformed > 0 {
		t.Fatalf("Read() malformed = %d, err = %v\n%s", malformed, err, data)
	}
	return lines
}

func TestRecordBlock(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	o := testOptions(t, root, ".astimate/blocks.jsonl")
	if err := Record(t.Context(), o, blocked()); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	got := readLog(t, filepath.Join(root, ".astimate", "blocks.jsonl"))
	want := []Line{
		{V: Version, Event: EventBlock, Time: "2026-11-15T10:00:00Z", Module: testModule, Package: "a", Metric: "exported_symbols",
			ConfigVersion: "cfg-1", AstimateVersion: "v0.test",
			Block: &Block{Kind: "capacity", Base: ptr(58), Head: 63, Limit: "max 60"}},
		{V: Version, Event: EventBlock, Time: "2026-11-15T10:00:00Z", Module: testModule, Package: "b", Metric: "sloc",
			ConfigVersion: "cfg-1", AstimateVersion: "v0.test",
			Block: &Block{Kind: "capacity", Head: 1200, Limit: "max 1000; tokens_est 17000, max 16000", Also: []string{"tokens_est"}}},
	}
	assertLines(t, got, want)
}

// assertLines compares lines by their JSON encoding.
func assertLines(t *testing.T, got, want []Line) {
	t.Helper()
	g, _ := json.MarshalIndent(got, "", "  ")
	w, _ := json.MarshalIndent(want, "", "  ")
	if !bytes.Equal(g, w) {
		t.Errorf("log lines =\n%s\nwant\n%s", g, w)
	}
}

func TestRecordPass(t *testing.T) {
	t.Parallel()

	block := func(module, pkg, metric string, also ...string) string {
		l := Line{V: Version, Event: EventBlock, Module: module, Package: pkg, Metric: metric,
			Block: &Block{Kind: "capacity", Head: 63, Limit: "max 60", Also: also}}
		data, _ := json.Marshal(l)
		return string(data) + "\n"
	}
	pass := func(pkg, metric string) string {
		l := Line{V: Version, Event: EventPass, Module: testModule, Package: pkg, Metric: metric, Pass: &Pass{Exists: true}}
		data, _ := json.Marshal(l)
		return string(data) + "\n"
	}
	// passing has a split out of a (a/store, seeded from a), a new
	// package, a with 40 exports and the reshaped heads-up, and b deleted.
	passing := func() *report.Check {
		a := row("a", 40, 600, false)
		a.Report.Warnings = []report.Finding{{Metric: "exported_symbols", Base: ptr(41), Head: 40, Limit: "exports reshaped, max 60"}}
		a.Reshaped = []string{"Store.SetLatest (signature changed)", "Options (unexported as options)"}
		store := row("a/store", 23, 300, false)
		store.Report.BaselineFrom = []string{"a"}
		fresh := row("c", 2, 20, true)
		return &report.Check{Packages: []report.CheckedPackage{a, store, fresh}, Deleted: []string{"b"}}
	}
	newPkgs := func(fromA bool) []NewPackage {
		return []NewPackage{{Package: "a/store", BaselineFrom: []string{"a"}, FromBlocked: fromA}, {Package: "c"}}
	}
	exportsExempt := exempt.Entry{Package: "a", Metric: "exported_symbols", Reason: "split planned"}
	tests := []struct {
		name       string
		log        string // "" means no file
		exemptions []exempt.Entry
		want       []Line
	}{
		{name: "no log", log: ""},
		{name: "no open block", log: block(testModule, "a", "exported_symbols") + pass("a", "exported_symbols")},
		{name: "open block on a package not judged", log: block(testModule, "z", "sloc")},
		{name: "open block of another module", log: block("example.com/other", "a", "exported_symbols")},
		{
			name: "open blocks", log: block(testModule, "a", "exported_symbols") + block(testModule, "b", "sloc", "tokens_est") +
				"not json\n",
			exemptions: []exempt.Entry{exportsExempt},
			want: []Line{
				{Package: "a", Metric: "exported_symbols", ConfigVersion: "cfg-1", Pass: &Pass{Value: ptr(40), Exists: true,
					NewPackages: newPkgs(true), Exempted: true, Reshaped: true,
					ReshapedNames: []string{"Store.SetLatest (signature changed)", "Options (unexported as options)"}}},
				{Package: "b", Metric: "sloc", ConfigVersion: "", Pass: &Pass{Exists: false, NewPackages: newPkgs(false)}},
			},
		},
		{
			name: "merged finding needs every rule exempted", log: block(testModule, "a", "sloc", "tokens_est"),
			exemptions: []exempt.Entry{{Package: "a", Metric: "sloc", Reason: "r"}},
			want: []Line{{Package: "a", Metric: "sloc", ConfigVersion: "cfg-1",
				Pass: &Pass{Value: ptr(600), Exists: true, NewPackages: newPkgs(true)}}},
		},
		{
			name: "expired exemption", log: block(testModule, "a", "exported_symbols"),
			exemptions: []exempt.Entry{{Package: "a", Metric: "exported_symbols", Reason: "r", Expires: "2026-11-14"}},
			want: []Line{{Package: "a", Metric: "exported_symbols", ConfigVersion: "cfg-1", Pass: &Pass{Value: ptr(40), Exists: true,
				NewPackages: newPkgs(true), Reshaped: true,
				ReshapedNames: []string{"Store.SetLatest (signature changed)", "Options (unexported as options)"}}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			path := filepath.Join(root, "blocks.jsonl")
			if tt.log != "" {
				if err := os.WriteFile(path, []byte(tt.log), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			o := testOptions(t, root, "blocks.jsonl", tt.exemptions...)
			if err := Record(t.Context(), o, passing()); err != nil {
				t.Fatalf("Record() error = %v", err)
			}
			data, _ := os.ReadFile(path)
			if !strings.HasPrefix(string(data), tt.log) {
				t.Fatalf("log no longer starts with what it held:\n%s", data)
			}
			if tt.log == "" && data != nil {
				t.Fatalf("Record() created the log with nothing open:\n%s", data)
			}
			lines, _, err := Read(strings.NewReader(strings.TrimPrefix(string(data), tt.log)))
			if err != nil {
				t.Fatal(err)
			}
			for i := range tt.want {
				w := &tt.want[i]
				w.V, w.Event, w.Time, w.Module, w.AstimateVersion = Version, EventPass, "2026-11-15T10:00:00Z", testModule, "v0.test"
				if w.ConfigVersion == "" {
					w.ConfigVersion = o.Config.Version // a deleted package has no row
				}
			}
			assertLines(t, lines, tt.want)
		})
	}
}

func TestRecordOff(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, c := range []*report.Check{blocked(), {}} {
		if err := Record(t.Context(), testOptions(t, root, ""), c); err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) > 0 {
		t.Errorf("Record() with the log off wrote %v (%v)", entries, err)
	}
}

func TestRecordUnwritable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// A file where the log's directory should be.
	if err := os.WriteFile(filepath.Join(root, "blocked"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := Record(t.Context(), testOptions(t, root, "blocked/blocks.jsonl"), blocked())
	if err == nil || !strings.Contains(err.Error(), "block log") {
		t.Errorf("Record() error = %v, want an error naming the block log", err)
	}
}

func TestGitState(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Parallel()

	repo := t.TempDir()
	gitIn := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com",
			"-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = repo
		cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool {
			name, _, _ := strings.Cut(kv, "=")
			return name == "GIT_DIR" || name == "GIT_WORK_TREE" || name == "GIT_INDEX_FILE"
		}), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, text string) {
		t.Helper()
		p := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn("init", "-q")
	logPath := filepath.Join(repo, ".astimate", "blocks.jsonl")
	if commit, dirty := gitState(t.Context(), repo, logPath); commit != "" || dirty != nil {
		t.Errorf("before the first commit: gitState() = %q, %v; want no commit and nil", commit, dirty)
	}
	write("a.go", "package a\n")
	gitIn("add", ".")
	gitIn("commit", "-q", "-m", "first")
	head := gitIn("rev-parse", "HEAD")

	tests := []struct {
		name  string
		edit  func()
		dirty bool
	}{
		{"clean", func() {}, false},
		{"the log itself is not a change", func() { write(".astimate/blocks.jsonl", "{}\n") }, false},
		{"an untracked file", func() { write("b.go", "package a\n") }, true},
		{"an edit", func() { write("a.go", "package a\n\nvar x int\n") }, true},
	}
	for _, tt := range tests { // sequential: each edit builds on the last
		tt.edit()
		commit, dirty := gitState(t.Context(), repo, logPath)
		if commit != head || dirty == nil || *dirty != tt.dirty {
			t.Errorf("%s: gitState() = %q, %v; want %q, %v", tt.name, commit, dirty, head, tt.dirty)
		}
	}
}

func TestRead(t *testing.T) {
	t.Parallel()

	good := `{"v":1,"event":"block","module":"m","package":"a","metric":"sloc","block":{"kind":"capacity","base":null,"head":2,"limit":"max 1"}}`
	tests := []struct {
		name      string
		in        string
		lines     int
		malformed int
	}{
		{"empty", "", 0, 0},
		{"one line", good + "\n", 1, 0},
		{"blank lines and no final newline", "\n" + good + "\n\n" + good, 2, 0},
		{"torn line", good + "\n" + good[:20], 1, 1},
		{"future version", strings.Replace(good, `"v":1`, `"v":9`, 1), 0, 1},
		{"unknown event", strings.Replace(good, `"block","module"`, `"note","module"`, 1), 0, 1},
		{"block without its object", `{"v":1,"event":"block","module":"m","package":"a","metric":"sloc"}`, 0, 1},
		{"pass with a block object", strings.Replace(good, `"event":"block"`, `"event":"pass"`, 1), 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lines, malformed, err := Read(strings.NewReader(tt.in))
			if err != nil || len(lines) != tt.lines || malformed != tt.malformed {
				t.Errorf("Read() = %d lines, %d malformed, %v; want %d, %d", len(lines), malformed, err, tt.lines, tt.malformed)
			}
		})
	}
}

// TestKeyOrder pins that pass lines come out sorted whatever the map
// order, so two runs over one log write identical lines.
func TestKeyOrder(t *testing.T) {
	t.Parallel()

	open := map[Key]Line{}
	var c report.Check
	for _, p := range []string{"c", "a", "b"} {
		for _, m := range []string{"sloc", "exported_symbols"} {
			open[Key{testModule, p, m}] = Line{Package: p, Metric: m, Block: &Block{}}
		}
		c.Packages = append(c.Packages, row(p, 1, 1, false))
	}
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	got := passLines(&c, open, cfg, testNow())
	keys := make([]string, 0, len(got))
	for _, l := range got {
		keys = append(keys, l.Package+" "+l.Metric)
	}
	if !slices.IsSorted(keys) || len(keys) != 6 {
		t.Errorf("pass lines in order %q, want all six sorted", keys)
	}
}
