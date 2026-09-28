package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/report"
)

// tsFixtureDir is the TypeScript fixture module, relative to this package's
// directory.
const tsFixtureDir = "../../testdata/ts/fixture"

// tsFixturePackages are the package directories of the TypeScript fixture.
func tsFixturePackages() []string {
	return []string{"a", "b", "dupes", "hidden", "hub", "tested", "trivial"}
}

func TestTypeScriptRank(t *testing.T) {
	t.Parallel()

	args := []string{"rank", tsFixtureDir, "--json"}
	var stdout, stderr bytes.Buffer
	if got := run(args, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, stderr.String())
	}
	var rows []report.Row
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rows); err != nil {
		t.Fatalf("decoding rows: %v\n%s", err, stdout.String())
	}
	if got := sorted(rowPaths(rows)...); !slices.Equal(got, tsFixturePackages()) {
		t.Errorf("rows = %q, want one per TypeScript fixture package %q", got, tsFixturePackages())
	}
	for i := range rows {
		if rows[i].Path == "hub" && rows[i].FanIn != 4 {
			t.Errorf("hub fan_in = %d, want 4", rows[i].FanIn)
		}
	}
}

// TestTypeScriptCheckSelfBaseline writes a baseline of the TypeScript
// fixture with the CLI, then checks every package against it: nothing got
// worse, so the gate passes.
func TestTypeScriptCheckSelfBaseline(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), "baseline.json")
	var stdout, stderr bytes.Buffer
	write := []string{"baseline", "write", tsFixtureDir, "--out", base}
	if got := run(write, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", write, got, exitOK, stderr.String())
	}
	if want := "(7 packages)"; !strings.Contains(stdout.String(), want) {
		t.Errorf("baseline write stdout = %q, want it to report %s", stdout.String(), want)
	}

	for _, format := range []string{formatText, formatJSON} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()

			var out, errOut bytes.Buffer
			args := []string{"check", tsFixtureDir, "--all", "--baseline", base, "--format", format}
			if got := run(args, &out, &errOut); got != exitOK {
				t.Fatalf("run(%q) exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s",
					args, got, exitOK, out.String(), errOut.String())
			}
			if format != formatJSON {
				return
			}
			reports := decodeReports(t, out.Bytes())
			paths := make([]string, 0, len(reports))
			for i := range reports {
				r := &reports[i]
				paths = append(paths, r.PackagePath)
				if r.Language != "typescript" {
					t.Errorf("%s: language = %q, want typescript", r.PackagePath, r.Language)
				}
				// The embedded default's typescript override judged it.
				if r.ConfigVersion != "thresholds-2026-09-28+typescript" {
					t.Errorf("%s: config_version = %q, want the typescript override's", r.PackagePath, r.ConfigVersion)
				}
			}
			if slices.Sort(paths); !slices.Equal(paths, tsFixturePackages()) {
				t.Errorf("checked packages = %q, want %q", paths, tsFixturePackages())
			}
		})
	}
}

func TestTypeScriptAssessPackage(t *testing.T) {
	t.Parallel()

	args := []string{"assess", filepath.Join(tsFixtureDir, "dupes"), "--json"}
	var stdout, stderr bytes.Buffer
	if got := run(args, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, stderr.String())
	}
	var r report.Report
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("decoding report: %v\n%s", err, stdout.String())
	}
	if r.PackagePath != "dupes" || r.Language != "typescript" || r.Metrics.DupBlocks != 1 {
		t.Errorf("report = path %q, language %q, dup_blocks %d; want dupes, typescript, 1",
			r.PackagePath, r.Language, r.Metrics.DupBlocks)
	}
}

// TestLanguageSelection checks that a directory resolves to the nearest
// module root any extractor detects, and that Go wins when a root holds
// both a go.mod and a package.json.
func TestLanguageSelection(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	root := writeModule(t)
	files := map[string]string{
		"package.json":     "{}\n",
		"a/a.ts":           "export const a = 1;\n",
		"web/package.json": "{}\n",
		"web/ui/ui.ts":     "export const ui = 1;\n",
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		dir, language, path string
	}{
		{"a", "go", "a"},
		{"web/ui", "typescript", "ui"},
	}
	for _, tc := range cases {
		t.Run(tc.dir, func(t *testing.T) {
			t.Parallel()

			args := []string{"assess", filepath.Join(root, filepath.FromSlash(tc.dir)), "--json"}
			var stdout, stderr bytes.Buffer
			if got := run(args, &stdout, &stderr); got != exitOK {
				t.Fatalf("run(%q) exit code = %d; stderr = %q", args, got, stderr.String())
			}
			var r report.Report
			if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
				t.Fatalf("decoding report: %v\n%s", err, stdout.String())
			}
			if r.Language != tc.language || r.PackagePath != tc.path {
				t.Errorf("language, path = %q, %q, want %q, %q", r.Language, r.PackagePath, tc.language, tc.path)
			}
		})
	}
}
