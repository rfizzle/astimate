package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
