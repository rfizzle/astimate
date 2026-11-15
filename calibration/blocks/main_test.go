package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/blocklog"
)

// updateFlag names the flag that makes TestGolden rewrite its golden
// files. Use it only after an intentional change to the output, then read
// the diff.
const updateFlag = "update"

// TestMain defines the -update flag before the test binary parses flags;
// TestGolden reads it back with flag.Lookup, so no package variable holds
// it.
func TestMain(m *testing.M) {
	flag.Bool(updateFlag, false, "rewrite the golden files")
	flag.Parse()
	os.Exit(m.Run())
}

const fixture = "testdata/blocks.jsonl"

// TestClassify checks each resolution on the fixture log, the first rule
// winning where two match.
func TestClassify(t *testing.T) {
	t.Parallel()

	lines, malformed, err := readFile(fixture)
	if err != nil || malformed != 1 {
		t.Fatalf("readFile() malformed = %d, err = %v; want 1 malformed line", malformed, err)
	}
	got := map[string]episode{}
	for _, e := range episodes(lines) {
		got[e.Package+" "+e.Metric] = e
	}
	tests := []struct {
		name       string
		key        string
		resolution string
		evidence   string
		blocks     int
	}{
		{"exemption beats a fall past the split threshold", "internal/api exported_symbols", resExempted, "an exemption covers", 1},
		{"rename from the blocked package", "internal/store sloc", resSplit, "internal/store/index was seeded by a rename", 2},
		{"capacity fell at least 25%", "internal/cache sloc", resSplit, "sloc fell 27% (1100 -> 800)", 1},
		{"deleted package", "internal/legacy sloc", resSplit, "internal/legacy was deleted", 1},
		{"split beats reshaped", "internal/plugin exported_symbols", resSplit, "exported_symbols fell 31% (64 -> 44)", 1},
		{"reshaped heads-up", "internal/registry exported_symbols", resReshaped, "named Store.SetLatest (signature changed)", 1},
		{"capacity fell less than 25%", "internal/query sloc", resReduced, "the log cannot tell which", 1},
		{"density fell", "internal/render dup_blocks", resReduced, "dup_blocks fell 33% (3 -> 2)", 1},
		{"module-wide value is not compared", "<module> dup_blocks_cross_pkg", resReduced, "full count", 1},
		{"requirement now holds", "internal/report has_tests", resReduced, "has_tests went from 0 to 1; the log cannot tell how", 1},
		{"value did not move", "internal/auth untested_exports", resUnexplained, "(config_version changed)", 1},
		{"no later pass", "internal/wire internal_imports", resOpen, "no check has passed", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e, ok := got[tt.key]
			if !ok {
				t.Fatalf("no episode for %s", tt.key)
			}
			if e.Resolution != tt.resolution || !strings.Contains(e.Evidence, tt.evidence) || e.Blocks != tt.blocks {
				t.Errorf("%s: %s (%q), %d blocks; want %s with evidence containing %q, %d blocks",
					tt.key, e.Resolution, e.Evidence, e.Blocks, tt.resolution, tt.evidence, tt.blocks)
			}
		})
	}
	if len(got) != len(tests) {
		t.Errorf("%d episodes, want %d: a pass with no open block must not start one", len(got), len(tests))
	}
}

// TestSeparateLogs checks that a pass in one log never resolves a block in
// another.
func TestSeparateLogs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logs := map[string]string{
		"a.jsonl": `{"v":1,"event":"block","module":"m","package":"p","metric":"sloc","block":{"kind":"capacity","base":null,"head":1200,"limit":"max 1000"}}`,
		"b.jsonl": `{"v":1,"event":"pass","module":"m","package":"p","metric":"sloc","pass":{"value":10,"exists":true,"new_packages":[],"exempted":false,"reshaped":false}}`,
	}
	for name, text := range logs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errOut bytes.Buffer
	if err := run([]string{"--json", filepath.Join(dir, "a.jsonl"), filepath.Join(dir, "b.jsonl")}, &out, &errOut); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.Contains(out.String(), `"resolution": "open"`) {
		t.Errorf("output =\n%s\nwant the block open", out.String())
	}
}

// TestGolden pins the table and the JSON on the fixture log; -update
// rewrites them.
func TestGolden(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, golden string }{
		{"text", "testdata/blocks.txt"},
		{"json", "testdata/blocks.json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args := []string{fixture}
			if tt.name == "json" {
				args = []string{"--json", fixture}
			}
			var out, errOut bytes.Buffer
			if err := run(args, &out, &errOut); err != nil {
				t.Fatalf("run() error = %v", err)
			}
			if !strings.Contains(errOut.String(), "1 malformed lines skipped") {
				t.Errorf("stderr = %q, want the malformed line counted", errOut.String())
			}
			if flag.Lookup(updateFlag).Value.String() == "true" {
				if err := os.WriteFile(tt.golden, out.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(tt.golden)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out.Bytes(), want) {
				t.Errorf("output differs from %s (rerun with -update):\n%s", tt.golden, out.String())
			}
		})
	}
}

func TestRunErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"--nope"}, "usage"},
		{"missing log", []string{filepath.Join(t.TempDir(), "none.jsonl")}, "opening block log"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out, errOut bytes.Buffer
			if err := run(tt.args, &out, &errOut); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("run(%q) error = %v, want one containing %q", tt.args, err, tt.want)
			}
		})
	}
}

// Keep the schema version the fixture was written at in step with the
// package: a version bump needs the fixture rewritten.
func TestFixtureVersion(t *testing.T) {
	t.Parallel()

	if blocklog.Version != 1 {
		t.Errorf("blocklog.Version = %d; regenerate %s for it", blocklog.Version, fixture)
	}
}
