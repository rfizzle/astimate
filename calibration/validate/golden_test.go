package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
)

// updateFlag names the flag that makes TestRunTwoLabelSets rewrite its
// golden report, testdata/two-label-sets.md. Use it only after an
// intentional change to the report, then read the result.
const updateFlag = "update"

// TestMain defines the -update flag before the test binary parses flags;
// the golden test reads it back with flag.Lookup, so no package variable
// holds it.
func TestMain(m *testing.M) {
	flag.Bool(updateFlag, false, "rewrite the validate report golden")
	flag.Parse()
	os.Exit(m.Run())
}

// secondSet writes a second labels file for the corpus value corpusArg
// (name=<dir>:<labels>) and returns the value naming both files.
func secondSet(t *testing.T, corpusArg, labelsYAML string) string {
	t.Helper()
	name, _, _ := strings.Cut(corpusArg, "=")
	path := name + "-split-extract.yaml"
	if err := os.WriteFile(path, []byte(labelsYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return corpusArg + "," + path
}

// TestRunTwoLabelSets renders the report of two corpora, each with a
// second, split-or-extraction label set, and compares it with the golden.
// The fixture is written with relative paths in a temporary working
// directory, so the report does not depend on where the test runs.
func TestRunTwoLabelSets(t *testing.T) {
	golden, err := filepath.Abs(filepath.Join("testdata", "two-label-sets.md"))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	hand, rule := corpora(t, ".")
	const split = "source: {repository: r, range: x, data: d}\nrule: block when a later commit splits a package this commit grew past a ceiling, or extracts duplicate code this commit added\ncommits:\n"
	hand = secondSet(t, hand, split+`  - {hash: h1, verdict: block, reason: split later, provenance: rule, fired: [{part: split, by: h9, package: a}]}
  - {hash: h2, verdict: allow, reason: never split, provenance: rule}
  - {hash: h3, verdict: block, reason: extracted later, provenance: rule, fired: [{part: extract, by: h9, package: a}]}
`)
	rule = secondSet(t, rule, split+`  - {hash: r1, verdict: allow, reason: nothing grew, provenance: rule, agent: true}
  - {hash: r2, verdict: allow, reason: nothing grew, provenance: rule, agent: true}
`)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--corpus", hand, "--corpus", rule, "--out", "report.md", "--date", "2026-09-29"}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v; stderr %s", err, stderr.String())
	}
	got, err := os.ReadFile("report.md")
	if err != nil {
		t.Fatal(err)
	}
	if flag.Lookup(updateFlag).Value.String() == "true" {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run with -%s to create it)", err, updateFlag)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("report differs from %s; rerun with -%s and read the diff:\n%s", golden, updateFlag, got)
	}
}

// candidateConfig writes the embedded default with a duplicated_lines
// rule added, at floor, as cand.yaml and returns its path.
func candidateConfig(t *testing.T, floor string) string {
	t.Helper()
	const anchor = "  - metric: untested_exports\n"
	def := string(config.Default())
	if !strings.Contains(def, anchor) {
		t.Fatal("the default has no untested_exports rule")
	}
	rule := "  - metric: duplicated_lines\n    kind: density\n    max_delta: 0\n    floor: " + floor + "\n"
	if err := os.WriteFile("cand.yaml", []byte(strings.Replace(def, anchor, rule+anchor, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	return "cand.yaml"
}

// TestRunCandidate renders the report of a corpus with a candidate rule
// on duplicated_lines, under two label sets, and compares it with the
// golden testdata/candidate.md: the candidate's rates, its sweep over the
// floor and the gate without and with it.
func TestRunCandidate(t *testing.T) {
	golden, err := filepath.Abs(filepath.Join("testdata", "candidate.md"))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	c := func(hash string, passed bool) string {
		return `{"commit":"` + hash + `","subject":"subject ` + hash + `","config_version":"` + cfg.Version + `","baseline":"parent","loaded":true,"passed":` + strconv.FormatBool(passed) + "}\n"
	}
	row := func(hash string, base, head string, violations string) string {
		return `{"commit":"` + hash + `","package":"a","language":"go","metrics":{"sloc":200,"has_tests":true,` + head + `},"base":{"sloc":190,"has_tests":true,` + base + `},"sloc_delta":10,"violations":[` + violations + "]}\n"
	}
	lines := fixture(t, ".", "lines",
		c("l1", true)+c("l2", true)+c("l3", true)+c("l4", false),
		row("l1", `"dup_blocks":6,"duplication_pct":83.3,"duplicated_lines":135`, `"dup_blocks":5,"duplication_pct":73.7,"duplicated_lines":157`, "")+
			row("l2", `"dup_blocks":1,"duplication_pct":6,"duplicated_lines":12`, `"dup_blocks":1,"duplication_pct":7.5,"duplicated_lines":15`, "")+
			row("l3", `"dup_blocks":2,"duplication_pct":25,"duplicated_lines":50`, `"dup_blocks":2,"duplication_pct":25,"duplicated_lines":50`, "")+
			row("l4", `"dup_blocks":1,"duplication_pct":5,"duplicated_lines":10`, `"dup_blocks":2,"duplication_pct":9,"duplicated_lines":40`, `{"metric":"dup_blocks"}`),
		`source: {repository: r, range: x, data: d}
commits:
  - {hash: l1, verdict: block, reason: a third copy merged two blocks, provenance: confirmed, agent: true}
  - {hash: l2, verdict: allow, reason: three lines of boilerplate, provenance: confirmed, agent: true}
  - {hash: l3, verdict: allow, reason: unchanged, provenance: confirmed, agent: true}
  - {hash: l4, verdict: block, reason: a paste, provenance: confirmed, agent: true}
`)
	const split = "source: {repository: r, range: x, data: d}\nrule: block when a later commit splits a package this commit grew past a ceiling, or extracts duplicate code this commit added\ncommits:\n"
	lines = secondSet(t, lines, split+`  - {hash: l1, verdict: allow, reason: never extracted, provenance: rule}
  - {hash: l2, verdict: allow, reason: never extracted, provenance: rule}
  - {hash: l3, verdict: allow, reason: never extracted, provenance: rule}
  - {hash: l4, verdict: block, reason: extracted later, provenance: rule, fired: [{part: extract, by: l9, package: a}]}
`)
	var stdout, stderr bytes.Buffer
	args := []string{"--corpus", lines, "--candidate", candidateConfig(t, "10"), "--out", "report.md", "--date", "2026-10-14"}
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v; stderr %s", err, stderr.String())
	}
	got, err := os.ReadFile("report.md")
	if err != nil {
		t.Fatal(err)
	}
	if flag.Lookup(updateFlag).Value.String() == "true" {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run with -%s to create it)", err, updateFlag)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("report differs from %s; rerun with -%s and read the diff:\n%s", golden, updateFlag, got)
	}
}

func TestRunCandidateErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	hand, _ := corpora(t, ".")
	if err := os.WriteFile("same.yaml", config.Default(), 0o644); err != nil {
		t.Fatal(err)
	}
	once, err := os.ReadFile(candidateConfig(t, "10"))
	if err != nil {
		t.Fatal(err)
	}
	const rule = "  - metric: duplicated_lines\n"
	twice := strings.Replace(string(once), rule, rule+"    kind: density\n    max_delta: 5\n"+rule, 1)
	if err := os.WriteFile("two.yaml", []byte(twice), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"no rule on an ungated metric": "same.yaml",
		"missing file":                 "nope.yaml",
		"two rules on one metric":      "two.yaml",
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run([]string{"--corpus", hand, "--candidate", path, "--out", "r.md"}, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), "--candidate") {
				t.Errorf("run error = %v, want a --candidate error", err)
			}
		})
	}
}

func TestRunSecondSetErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	hand, rule := corpora(t, ".")
	bad := secondSet(t, hand, "source: {repository: r, range: x, data: d}\ncommits:\n  - {hash: h1, verdict: allow, reason: x, provenance: rule}\n")
	for name, args := range map[string][]string{
		"second set covers too few commits": {"--corpus", bad, "--corpus", rule},
		"only one corpus names a second":    {"--corpus", secondSet(t, rule, "source: {repository: r, range: x, data: d}\ncommits:\n  - {hash: r1, verdict: allow, reason: x, provenance: rule}\n  - {hash: r2, verdict: allow, reason: x, provenance: rule}\n"), "--corpus", hand},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := run(append(args, "--out", "r.md"), &stdout, &stderr); err == nil {
				t.Error("run succeeded")
			}
		})
	}
}
