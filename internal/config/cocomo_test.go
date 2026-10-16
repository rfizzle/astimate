package config

import (
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// shippedCocomo is the rebuild profile default.yaml ships.
func shippedCocomo() score.Cocomo {
	c := score.Cocomo{Model: score.ModelCOCOMOII}
	for _, k := range score.CocomoKeys() {
		c.Set(k, "nominal")
	}
	c.Set("prec", "very_high")
	c.Set("flex", "very_low")
	c.Set("resl", "extra_high")
	c.Set("docu", "very_low")
	return c
}

func TestParseCocomoDefault(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"default", "without the section"} {
		data := string(Default())
		if name != "default" {
			start := strings.Index(data, "  # The human estimate's model and rebuild profile")
			end := strings.Index(data, "    sced: nominal\n") + len("    sced: nominal\n")
			data = data[:start] + data[end:]
		}
		cfg, err := Parse([]byte(data))
		if err != nil {
			t.Fatalf("%s: Parse error = %v", name, err)
		}
		if got := cfg.Rebuild.Cocomo; got != shippedCocomo() {
			t.Errorf("%s: Cocomo = %+v, want %+v", name, got, shippedCocomo())
		}
	}
}

func TestParseCocomoErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, old, repl, wantErr string
	}{
		{"unknown rating name", "prec: very_high", "prec: hgh", `rebuild: cocomo.prec: unknown rating "hgh"`},
		{"rating the driver lacks", "sced: nominal", "sced: extra_high", "cocomo.sced: extra_high is not a rating of sced"},
		{"unknown model", "model: cocomo-ii", "model: cocomo-iii", "cocomo.model must be cocomo-ii or basic-81"},
		{"per-package driver", "    docu: very_low\n", "    docu: very_low\n    rely: high\n", "rebuild.cocomo.rely: unknown key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(replace(t, tt.old, tt.repl)))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Parse error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestCocomoRatingMovesEstimate checks that a rating in the file reaches the
// human estimate, and the model switch with it.
func TestCocomoRatingMovesEstimate(t *testing.T) {
	t.Parallel()

	m := metrics.RawMetrics{SLOC: 4000, HasTests: true, FuncCount: 20, CognitiveP90: 6}
	days := func(old, repl string) float64 {
		t.Helper()
		cfg, err := Parse([]byte(replace(t, old, repl)))
		if err != nil {
			t.Fatal(err)
		}
		return score.Estimate(m, cfg.Rebuild).HumanDays
	}
	shipped := days("docu: very_low", "docu: very_low")
	if got := days("docu: very_low", "docu: nominal"); got == shipped {
		t.Errorf("docu nominal left human_days at %v", got)
	}
	if got := days("pmat: nominal", "pmat: extra_high"); got >= shipped {
		t.Errorf("pmat extra_high gave %v, want below %v", got, shipped)
	}
	// 2.4 * 4^1.05 * 19 = 195.49.
	if got := days("model: cocomo-ii", "model: basic-81"); got < 195.4 || got > 195.6 {
		t.Errorf("basic-81 human_days = %v, want 195.49", got)
	}
}

func TestForLanguageCocomo(t *testing.T) {
	t.Parallel()

	cfg, err := Parse(withLanguages("languages:\n  typescript:\n    rebuild:\n      cocomo:\n        docu: nominal\n"))
	if err != nil {
		t.Fatal(err)
	}
	ts := cfg.ForLanguage("typescript")
	want := shippedCocomo()
	want.Set("docu", "nominal")
	if ts.Rebuild.Cocomo != want {
		t.Errorf("typescript Cocomo = %+v, want docu nominal over the top level", ts.Rebuild.Cocomo)
	}
	if ts.Version != cfg.Version+"+typescript" {
		t.Errorf("typescript Version = %q, want the override suffix", ts.Version)
	}
	if got := cfg.ForLanguage("go").Rebuild.Cocomo; got != shippedCocomo() {
		t.Errorf("go Cocomo = %+v, want the top level", got)
	}
	if _, err := Parse(withLanguages("languages:\n  typescript:\n    rebuild:\n      cocomo:\n        docu: lots\n")); err == nil ||
		!strings.Contains(err.Error(), "languages.typescript.rebuild: cocomo.docu") {
		t.Errorf("bad override rating error = %v", err)
	}
	if _, err := Parse(withLanguages("languages:\n  typescript:\n    rebuild:\n      cocomo:\n        cplx: high\n")); err == nil ||
		!strings.Contains(err.Error(), "languages.typescript.rebuild.cocomo.cplx: unknown key") {
		t.Errorf("unknown override key error = %v", err)
	}
}
