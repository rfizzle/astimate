package term

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/report"
)

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }

// sample is a module report with one package of each band, a long path,
// and a failing change with warnings, an exemption, a deleted package and
// a notice.
func sample() *narrate.Module {
	pkg := func(path, band string, days float64, lines int, over ...string) narrate.Package {
		return narrate.Package{Path: path, Band: band, Over: over, Hotspot: days * 2,
			Rebuild: narrate.Rebuild{HumanDays: days, HumanTime: "2 weeks", HumanShort: "1 d"},
			Copies:  narrate.Copies{Pct: 12.5, Lines: lines}, Tests: narrate.Tests{Untested: 3},
			Churn: &narrate.Churn{Commits: 2}, Story: "Story of " + path + ".", Todo: "Todo of " + path + "."}
	}
	long := "internal/a/very/deeply/nested/package/whose/path/is/far/too/long/for/the/table"
	return &narrate.Module{
		Name: "Demo", ModulePath: "example.com/demo", ConfigVersion: "v1", ChurnWindow: 200,
		Counts:   narrate.Counts{Packages: 3, Split: 1, Watch: 1, Fine: 1, WithoutTests: 1, HumanTime: "3 weeks"},
		Headline: "Demo has 1 package too big to rebuild in one go. Start with " + long + ".",
		Hotspots: []string{long, "w", "f"},
		Packages: []narrate.Package{pkg(long, narrate.BandSplit, 9, 1200, "sloc"), pkg("w", narrate.BandWatch, 4, 800),
			pkg("f", narrate.BandFine, 1, 10)},
		Change: &narrate.Change{
			Base: "main", Verdict: "FAIL", Headline: "1 package needs attention before this merges.",
			Changed: 3, Failed: 1, Violations: 2, Warnings: 2, Passed: 1,
			Cross: &narrate.Cross{Base: ptr(1.0), Head: 2},
			Failing: []narrate.Failing{{Path: long, Summary: "2 findings · 9 days to rebuild · 2 changes lately",
				Findings: []narrate.Finding{
					{Label: "sloc", Base: ptr(1100.0), Head: 1300, Allowed: ptr(150.0), OverBy: ptr(300.0),
						MovedShort: "grew 200, allowed 150", Why: "Why sloc.", Do: "Do sloc.",
						Location: &report.Location{File: long + "/file.go", Line: 1}},
					{Label: "globals", Base: ptr(0.0), Head: 1, Limit: "max_delta +0", Do: "Do globals.",
						Location: &report.Location{File: long + "/g.go", Line: 9}},
				}}},
			WarningGroups: []narrate.WarningGroup{{Label: "internal imports", Why: "Why imports.",
				Packages: []narrate.Warned{{Path: "w", Base: ptr(9.0), Head: 10, Limit: "warn_at 0.75", Do: "Do imports."},
					{Path: "f", Head: 8, Limit: "warn_at 0.75"}}}},
			Exempted: []narrate.Exempted{{Path: "f", Label: "globals", Reason: "Kept on purpose."}},
			Deleted:  []string{"gone"},
			Notices:  []string{"remote skipped, it does not load (missing module): no such module"},
		},
	}
}

// write renders m with opts.
func write(t *testing.T, m *narrate.Module, opts narrate.WriteOptions) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Write(&buf, m, opts); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// TestWriteWidths checks every width's layout: no line passes the width
// (colour off, so every rune is a column), the copied and untested
// columns only from 100 columns, a path cut from the left, the finding's
// why and where at every location only above 80 columns, and the
// sentences of the report printed whole.
func TestWriteWidths(t *testing.T) {
	t.Parallel()

	for _, width := range []int{60, 80, 99, 100, 120} {
		out := write(t, sample(), narrate.WriteOptions{Width: width})
		for l := range strings.SplitSeq(out, "\n") {
			// A word longer than the width, such as a long path in a
			// sentence, gets a line of its own and may pass it.
			if runes(l) > width && len(strings.Fields(l)) > 1 {
				t.Errorf("width %d: line of %d columns: %q", width, runes(l), l)
			}
		}
		if got := strings.Contains(out, "COPIED"); got != (width >= 100) {
			t.Errorf("width %d: copied column shown %v", width, got)
		}
		if !strings.Contains(out, " …") {
			t.Errorf("width %d: no path cut from the left:\n%s", width, out)
		}
		flat := strings.Join(strings.Fields(out), " ")
		narrow := width <= narrowWidth
		if got := strings.Contains(flat, "Why sloc.") && strings.Contains(out, "/g.go:9"); got == narrow {
			t.Errorf("width %d: why and every location shown %v, want %v:\n%s", width, got, !narrow, out)
		}
		for _, want := range []string{sample().Headline, "Story of w.", "Do this → Todo of w.", "FAIL",
			"1 package needs attention before this merges.", "grew 200, allowed 150", "SPLIT", "WATCH", "FINE",
			"✗", "! 2 heads up: internal imports 2", "exempted f globals Kept on purpose.",
			"deleted since baseline: gone", "remote skipped, it does not load (missing module): no such module"} {
			if !strings.Contains(flat, want) {
				t.Errorf("width %d: output lacks %q:\n%s", width, want, out)
			}
		}
		if strings.Contains(out, "\x1b[") || strings.Contains(out, "█") {
			t.Errorf("width %d: colour or bars without colour:\n%s", width, out)
		}
	}
}

// TestWriteColor checks that colour adds escape codes and bars and keeps
// every word, so the text reads the same without it.
func TestWriteColor(t *testing.T) {
	t.Parallel()

	plain := write(t, sample(), narrate.WriteOptions{Width: 120})
	color := write(t, sample(), narrate.WriteOptions{Width: 120, Color: true})
	if !strings.Contains(color, "\x1b[1;31mFAIL\x1b[0m") || !strings.Contains(color, "█") {
		t.Errorf("colour output lacks the red FAIL or the bars:\n%s", color)
	}
	stripped := strings.Fields(stripColor(color))
	for _, w := range strings.Fields(plain) {
		if !strings.Contains(strings.Join(stripped, " "), w) {
			t.Errorf("colour output lacks the word %q", w)
		}
	}
}

// stripColor drops the escape codes and bar runes of s.
func stripColor(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return strings.NewReplacer("█", "", "░", "").Replace(b.String())
}

// TestWriteChangeOnly checks the change report alone, as check prints it:
// no repository report, the exit code last, and with Warnings each warned
// package with its limit and what to do.
func TestWriteChangeOnly(t *testing.T) {
	t.Parallel()

	out := write(t, sample(), narrate.WriteOptions{Width: 100, ChangeOnly: true, Warnings: true, Exit: ptr(3)})
	flat := strings.Join(strings.Fields(out), " ")
	if !strings.HasPrefix(out, "FAIL ") || !strings.HasSuffix(out, "\n exit 3\n") || strings.Contains(out, "Every package") {
		t.Errorf("change report alone = \n%s", out)
	}
	for _, want := range []string{"! 2 heads up they never block", "internal imports Why imports.",
		"w 9 → 10 warn_at 0.75. Do imports.", "f 8 warn_at 0.75", "against main", "copies between packages 1 → 2"} {
		if !strings.Contains(flat, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// TestWritePass checks a change with nothing failing and a module with no
// packages: PASS first, no failing package, no table.
func TestWritePass(t *testing.T) {
	t.Parallel()

	m := &narrate.Module{Name: "Empty", Headline: "Empty has no package too big to rebuild in one go.",
		Change: &narrate.Change{Verdict: "PASS", Headline: "Nothing needs attention before this merges.", BaseCommit: strings.Repeat("ab", 20)}}
	out := write(t, m, narrate.WriteOptions{ChangeOnly: true})
	if !strings.HasPrefix(out, "PASS  Nothing needs attention before this merges.  against abababab\n") ||
		strings.Contains(out, "✗") || strings.Contains(out, "!") {
		t.Errorf("pass = \n%s", out)
	}
	out = write(t, m, narrate.WriteOptions{})
	if !strings.HasPrefix(out, "Empty\n\nEmpty has no package") || strings.Contains(out, "Every package") ||
		strings.Contains(out, "Start here") || !strings.Contains(out, "\nPASS ") {
		t.Errorf("empty module = \n%s", out)
	}
}

func TestWrap(t *testing.T) {
	t.Parallel()

	got := wrap("one two three averyveryverylongword four", 10)
	want := []string{"one two", "three", "averyveryverylongword", "four"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("wrap = %q, want %q", got, want)
	}
	if got := truncLeft("abcdefgh", 5); got != "…efgh" {
		t.Errorf("truncLeft = %q, want …efgh", got)
	}
}
