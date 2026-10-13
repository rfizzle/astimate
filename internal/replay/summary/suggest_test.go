package summary

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
)

// TestSuggest checks that a suggestion parses, sets warn on exactly the
// rules on the metrics asked for, at the top level and in the typescript
// override alike, replaces an explicit fail, leaves a disabling rule alone
// and keeps the file's comments.
func TestSuggest(t *testing.T) {
	t.Parallel()

	src := config.Default()
	// An explicit severity on a rule to warn on, and a language that
	// disables a metric to warn on.
	src = bytes.Replace(src, []byte("  - metric: sloc\n    kind: capacity\n"),
		[]byte("  - metric: sloc\n    severity: fail\n    kind: capacity\n"), 1)
	src = bytes.Replace(src, []byte("\nlanguages:\n"),
		[]byte("\nlanguages:\n  go:\n    thresholds:\n      - metric: globals\n        disabled: true\n"), 1)
	if _, err := config.Parse(src); err != nil {
		t.Fatalf("test configuration: %v", err)
	}
	warn := []string{"globals", "sloc", "tokens_est"}
	out, err := Suggest(src, warn)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse(out)
	if err != nil {
		t.Fatalf("suggestion does not parse: %v\n%s", err, out)
	}
	check := func(where string, rules []gate.Threshold) {
		for _, r := range rules {
			if want := slices.Contains(warn, r.Metric); r.Warns() != want {
				t.Errorf("%s rule %s: warns %v, want %v", where, r.Metric, r.Warns(), want)
			}
		}
	}
	check("top-level", cfg.Thresholds)
	check("typescript", cfg.ForLanguage("typescript").Thresholds)
	check("go", cfg.ForLanguage("go").Thresholds)
	for _, comment := range []string{"# Identifies the calibration the values came from", "# " + suggestComment} {
		if !strings.Contains(string(out), comment) {
			t.Errorf("suggestion lacks %q", comment)
		}
	}

	if same, err := Suggest(src, nil); err != nil || !bytes.Equal(same, src) {
		t.Errorf("suggestion without rules over budget changed the file (err %v)", err)
	}
}
