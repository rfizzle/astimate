package split

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/replay/labels"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// capacity is one capacity rule, sloc at most 1000, for every language.
func capacity(string) []gate.Threshold {
	limit := 1000.0
	return []gate.Threshold{{Metric: "sloc", Kind: gate.Capacity, Max: &limit}}
}

// pkg is a row of package p going from base sloc and duplicate blocks to
// head ones; a negative base sloc makes the row new.
func pkg(p string, baseSLOC, baseDup, headSLOC, headDup int) Row {
	r := Row{Package: p, Language: "go", Metrics: metrics.RawMetrics{SLOC: headSLOC, DupBlocks: headDup}}
	if baseSLOC >= 0 {
		r.Base = &metrics.RawMetrics{SLOC: baseSLOC, DupBlocks: baseDup}
		r.SLOCDelta = headSLOC - baseSLOC
	} else {
		r.SLOCDelta = headSLOC
	}
	return r
}

// moduleRow is the module row with cross-package duplicates going from
// base to head.
func moduleRow(base, head int) Row {
	return Row{Package: metrics.ModuleRowID, Language: "go",
		Metrics: metrics.RawMetrics{DupBlocksCrossPkg: &head}, Base: &metrics.RawMetrics{DupBlocksCrossPkg: &base}}
}

// step is one synthetic commit: its rows, the files it changed, the
// packages it changed and those it deleted.
type step struct {
	rows              []Row
	files, pkgs, dels []string
}

// replay builds a Replay of steps, commits named c0, c1, ... The first
// commit also carries a large package big that nothing changes again, so
// the module total holds more than the package under test.
func replay(steps ...step) *Replay {
	commits := make([]Commit, len(steps))
	rows := map[string][]Row{}
	for i, s := range steps {
		h := "c" + string(rune('0'+i))
		commits[i] = Commit{Commit: h, Subject: "subject " + h, Loaded: true,
			FilesChanged: s.files, PackagesChanged: s.pkgs, PackagesDeleted: s.dels}
		rows[h] = s.rows
	}
	rows["c0"] = append(rows["c0"], pkg("big", 5000, 0, 5000, 0))
	return NewReplay(commits, rows, ".", capacity)
}

// quiet is a commit that changes no package.
func quiet() step { return step{files: []string{"README.md"}} }

func TestDecide(t *testing.T) {
	tests := []struct {
		name    string
		r       *Replay
		window  int
		want    []string // part@by per commit, "" for allow
		reasons []string // substrings of each commit's reason
	}{
		{
			name: "split",
			r: replay(
				step{rows: []Row{pkg("a", 900, 0, 1200, 0)}, files: []string{"a/a.go"}},
				quiet(),
				step{rows: []Row{pkg("a", 1200, 0, 500, 0), pkg("a/b", -1, 0, 700, 0)}, files: []string{"a/a.go", "a/b/b.go"}},
			),
			want:    []string{"split@c2", "", ""},
			reasons: []string{"took a over sloc max 1000 (900 to 1200)", "(lookahead 1 commits)", "(lookahead 0 commits)"},
		},
		{
			name: "growth over a ceiling, split by deleting the package into another",
			r: replay(
				step{rows: []Row{pkg("a", 1100, 0, 1250, 0)}},
				step{rows: []Row{pkg("c", -1, 0, 1250, 0)}, dels: []string{"a"}},
			),
			want:    []string{"split@c1", ""},
			reasons: []string{"added 150 SLOC to a, already over sloc 1000", ""},
		},
		{
			name: "deletion: the module total falls, not a split",
			r: replay(
				step{rows: []Row{pkg("a", 900, 0, 1200, 0)}},
				step{rows: []Row{pkg("a", 1200, 0, 300, 0)}},
			),
			want:    []string{"", ""},
			reasons: []string{"took a over sloc max 1000 (900 to 1200); no later commit split or extracted it", ""},
		},
		{
			name: "growth never split",
			r: replay(
				step{rows: []Row{pkg("a", 1100, 0, 1250, 0)}},
				step{rows: []Row{pkg("a", 1250, 0, 1240, 0)}},
			),
			want:    []string{"", ""},
			reasons: []string{"added 150 SLOC to a, already over sloc 1000; no later commit split or extracted it", "took no package over"},
		},
		{
			name: "growth under 100 SLOC over a ceiling is not counted",
			r: replay(
				step{rows: []Row{pkg("a", 1100, 0, 1150, 0)}},
				step{rows: []Row{pkg("a", 1150, 0, 500, 0), pkg("d", -1, 0, 650, 0)}},
			),
			want: []string{"", ""},
		},
		{
			name: "extraction",
			r: replay(
				step{rows: []Row{pkg("a", 100, 0, 140, 2)}, files: []string{"a/a.go"}},
				step{rows: []Row{pkg("a", 140, 2, 120, 0)}, files: []string{"a/a.go"}},
			),
			want:    []string{"extract@c1", ""},
			reasons: []string{"extract: added 2 duplicate blocks to a (0 to 2); c1 (\"subject c1\") 1 commits later removed 2 duplicate blocks from a"},
		},
		{
			name: "extraction must remove as many as were added",
			r: replay(
				step{rows: []Row{pkg("a", 100, 0, 140, 2)}, files: []string{"a/a.go"}},
				step{rows: []Row{pkg("a", 140, 2, 130, 1)}, files: []string{"a/a.go"}},
			),
			want: []string{"", ""},
		},
		{
			name: "extraction must not delete a quarter of the package",
			r: replay(
				step{rows: []Row{pkg("a", 100, 0, 140, 2)}, files: []string{"a/a.go"}},
				step{rows: []Row{pkg("a", 140, 2, 60, 0)}, files: []string{"a/a.go"}},
			),
			want: []string{"", ""},
		},
		{
			name: "extraction needs a change to a file of the package",
			r: replay(
				step{rows: []Row{pkg("a", 100, 0, 140, 2)}, files: []string{"a/a.go"}},
				step{rows: []Row{pkg("a", 140, 2, 140, 0)}, files: []string{"a/sub/x.go"}},
			),
			want: []string{"", ""},
		},
		{
			name: "cross-package extraction touches a package the commit changed",
			r: replay(
				step{rows: []Row{pkg("a", 100, 0, 120, 0), moduleRow(3, 5)}, files: []string{"a/a.go"}, pkgs: []string{"a"}},
				step{rows: []Row{pkg("e", 50, 0, 60, 0), moduleRow(5, 3)}, files: []string{"e/e.go"}, pkgs: []string{"e"}},
				step{rows: []Row{pkg("a", 120, 0, 110, 0), moduleRow(5, 3)}, files: []string{"a/a.go"}, pkgs: []string{"a"}},
			),
			want:    []string{"extract@c2", "", ""},
			reasons: []string{"added 2 cross-package duplicate blocks (3 to 5)"},
		},
		{
			name:   "window boundary: a split at the window's last commit counts",
			window: 2,
			r: replay(
				step{rows: []Row{pkg("a", 900, 0, 1200, 0)}},
				quiet(),
				step{rows: []Row{pkg("a", 1200, 0, 500, 0), pkg("f", -1, 0, 700, 0)}},
			),
			want:    []string{"split@c2", "", ""},
			reasons: []string{"(lookahead 2 commits)"},
		},
		{
			name:   "window boundary: a split past the window does not",
			window: 1,
			r: replay(
				step{rows: []Row{pkg("a", 900, 0, 1200, 0)}},
				quiet(),
				step{rows: []Row{pkg("a", 1200, 0, 500, 0), pkg("f", -1, 0, 700, 0)}},
			),
			want:    []string{"", "", ""},
			reasons: []string{"(lookahead 1 commits)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Decide(tt.r, tt.window)
			for i, l := range got {
				var fired []string
				for _, e := range l.Fired {
					fired = append(fired, e.Part+"@"+e.By)
				}
				if s := strings.Join(fired, ","); s != tt.want[i] {
					t.Errorf("commit %d fired %q, want %q (reason %q)", i, s, tt.want[i], l.Reason)
				}
				if v := map[bool]labels.Verdict{true: labels.Block, false: labels.Allow}[tt.want[i] != ""]; l.Verdict != v || l.Provenance != labels.Rule {
					t.Errorf("commit %d: %s %s", i, l.Verdict, l.Provenance)
				}
				if i < len(tt.reasons) && !strings.Contains(l.Reason, tt.reasons[i]) {
					t.Errorf("commit %d reason %q lacks %q", i, l.Reason, tt.reasons[i])
				}
			}
		})
	}
}

func TestNotLoaded(t *testing.T) {
	r := NewReplay([]Commit{{Commit: "c0"}}, nil, ".", capacity)
	if l := Decide(r, 0)[0]; l.Verdict != labels.Allow || !strings.Contains(l.Reason, "did not load") {
		t.Errorf("label %+v", l)
	}
}

func TestTouchesModuleDir(t *testing.T) {
	r := NewReplay([]Commit{{Commit: "c0", FilesChanged: []string{"mod/a/a.go", "a/b.go", "mod/x.go"}}}, nil, "mod", capacity)
	for p, want := range map[string]bool{"a": true, ".": true, "b": false} {
		if got := r.touches(0, p); got != want {
			t.Errorf("touches(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestRuleText(t *testing.T) {
	if s := RuleText(0); !strings.Contains(s, "in the rest of the replayed range") || strings.Contains(s, "%!") {
		t.Errorf("unbounded rule text %q", s)
	}
	if s := RuleText(40); !strings.Contains(s, "within the next 40 first-parent commits") {
		t.Errorf("windowed rule text %q", s)
	}
}

func TestReadRun(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadRun(dir); err == nil {
		t.Error("ReadRun without run.json succeeded")
	}
	if err := os.WriteFile(filepath.Join(dir, "run.json"), []byte(`{"source":{"range":"a..b","remote":"https://x/y.git","repository":"y"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	run, err := ReadRun(dir)
	if err != nil || run != (Run{Range: "a..b", Remote: "https://x/y.git", Repository: "y", ModuleDir: "."}) {
		t.Errorf("ReadRun = %+v, %v", run, err)
	}
}

func TestLoad(t *testing.T) {
	def, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	write := func(dir, name, s string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	write(dir, "run.json", `{"source":{"module_dir":"."}}`)
	write(dir, "commits.jsonl", `{"commit":"c0","config_version":"`+def.Version+`","loaded":true,"files_changed":["a/a.go"]}
{"commit":"c1","config_version":"`+def.Version+`","loaded":true,"files_changed":["a/a.go","a/b/b.go"]}
`)
	write(dir, "packages.jsonl", `{"commit":"c0","package":"a","language":"go","metrics":{"sloc":1200},"base":null,"sloc_delta":1200}

{"commit":"c1","package":"a","language":"go","metrics":{"sloc":500},"base":{"sloc":1200},"sloc_delta":-700}
{"commit":"c1","package":"a/b","language":"go","metrics":{"sloc":700},"base":null,"sloc_delta":700}
`)
	r, _, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := Decide(r, 0); got[0].Verdict != labels.Block || got[0].Fired[0].Package != "a" {
		t.Errorf("labels %+v", got)
	}
	other := filepath.Join(t.TempDir(), "astimate.yaml")
	write(filepath.Dir(other), "astimate.yaml", strings.Replace(string(config.Default()), "config_version: "+def.Version, "config_version: other-1", 1))
	if _, _, err := Load(dir, other); err == nil || !strings.Contains(err.Error(), "replay gated with") {
		t.Errorf("Load with another configuration: %v", err)
	}
	write(dir, "packages.jsonl", "not json\n")
	if _, _, err := Load(dir, ""); err == nil || !strings.Contains(err.Error(), "record 1") {
		t.Errorf("Load of a bad row: %v", err)
	}
	if _, _, err := Load(t.TempDir(), ""); err == nil {
		t.Error("Load of an empty directory succeeded")
	}
}
