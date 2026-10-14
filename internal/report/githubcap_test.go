package report

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// manyFindingsCheck returns a check of n packages, pkg00 on, each with one
// violation and one warning.
func manyFindingsCheck(n int) *Check {
	c := &Check{}
	for i := range n {
		path := "pkg" + strconv.Itoa(i/10) + strconv.Itoa(i%10)
		r := Build(&Input{Language: "go", PackagePath: path, ModulePath: "example.com/app",
			Metrics: metrics.RawMetrics{DupBlocks: 2}, Params: params(), ConfigVersion: uncalibratedVersion})
		ApplyGate(&r, "a1b2c3d", &metrics.RawMetrics{}, &gate.Result{
			Violations: []gate.Violation{{Metric: "dup_blocks", Base: 0, Head: 2, HasBase: true, Limit: "max_delta +0", Suggestion: "Extract helpers."}},
			Warnings:   []gate.Warning{{Metric: "globals", Head: 1, Limit: "max_delta +0", Suggestion: "Pass it explicitly."}},
		})
		c.Packages = append(c.Packages, CheckedPackage{Report: r})
	}
	return c
}

// TestWriteGitHubCap checks 25 findings and 25 warnings become ten
// annotations of each kind and a closing line saying how many are not.
func TestWriteGitHubCap(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WriteGitHub(&buf, manyFindingsCheck(25)); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if n := strings.Count(got, "::error "); n != 10 {
		t.Errorf("%d ::error annotations, want 10", n)
	}
	if n := strings.Count(got, "::warning "); n != 10 {
		t.Errorf("%d ::warning annotations, want 10", n)
	}
	if want := "astimate: 15 more findings and 15 more warnings are not annotated; the report lists them all\n"; !strings.HasSuffix(got, want) {
		t.Errorf("output ends %q, want %q", got[max(0, len(got)-len(want)-20):], want)
	}
	compareGolden(t, "testdata/cap/github.golden", got)
}

// TestWriteGitHubRankedOrder checks the rows named in rank come first, in
// that order, and the rest follow in check order; and that a check within
// the cap reports nothing omitted.
func TestWriteGitHubRankedOrder(t *testing.T) {
	t.Parallel()

	c := manyFindingsCheck(25)
	var buf bytes.Buffer
	omitted, err := WriteGitHubRanked(&buf, c, []string{"pkg24", "pkg03"})
	if err != nil {
		t.Fatal(err)
	}
	if omitted != (Omitted{Findings: 15, Warnings: 15}) {
		t.Errorf("omitted = %+v, want 15 findings and 15 warnings", omitted)
	}
	var files []string
	for l := range strings.SplitSeq(buf.String(), "\n") {
		if rest, ok := strings.CutPrefix(l, "::error file="); ok {
			files = append(files, strings.SplitN(rest, "::", 2)[0])
		}
	}
	if got, want := strings.Join(files, " "), "pkg24 pkg03 pkg00 pkg01 pkg02 pkg04 pkg05 pkg06 pkg07 pkg08"; got != want {
		t.Errorf("error order = %q, want %q", got, want)
	}

	buf.Reset()
	omitted, err = WriteGitHubRanked(&buf, manyFindingsCheck(10), nil)
	if err != nil || omitted != (Omitted{}) || OmittedText(omitted) != "" {
		t.Errorf("ten findings: omitted %+v, err %v, want none", omitted, err)
	}
}
