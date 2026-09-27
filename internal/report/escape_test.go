package report

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// arrow is a suggestion that HTML escaping would turn into
// "a -\u003e b \u0026 \u003cc\u003e".
const arrow = "a -> b & <c>"

// arrowCheck returns a check with one failing package whose violation
// suggestion is arrow.
func arrowCheck() *Check {
	r := Build(&Input{Language: "go", PackagePath: "p", ModulePath: "example.com/app",
		Metrics: metrics.RawMetrics{Globals: 1}, Params: params()})
	ApplyGate(&r, "a1b2c3d", &metrics.RawMetrics{}, &gate.Result{
		Violations: []gate.Violation{
			{Metric: "globals", Head: 1, HasBase: true, Limit: "max_delta +0", Suggestion: arrow},
		},
	})
	return &Check{Packages: []CheckedPackage{{Report: r}}}
}

func TestJSONWritersDoNotEscapeHTML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		write func(io.Writer) error
	}{
		{"WriteJSON", func(w io.Writer) error {
			r := Build(&Input{Language: "go", PackagePath: "p", ModulePath: "example.com/app", Params: params()})
			r.Suggestions = []string{arrow}
			return WriteJSON(w, &r)
		}},
		{"WriteRowsJSON", func(w io.Writer) error {
			return WriteRowsJSON(w, []Row{{Path: arrow}})
		}},
		{"WriteCheckJSON", func(w io.Writer) error {
			return WriteCheckJSON(w, arrowCheck())
		}},
		{"WriteHook", func(w io.Writer) error {
			return WriteHook(w, io.Discard, arrowCheck())
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			if err := tt.write(&buf); err != nil {
				t.Fatal(err)
			}
			out := buf.String()
			if !strings.Contains(out, arrow) {
				t.Errorf("output does not contain %q raw:\n%s", arrow, out)
			}
			for _, esc := range []string{`\u003c`, `\u003e`, `\u0026`} {
				if strings.Contains(out, esc) {
					t.Errorf("output contains %s:\n%s", esc, out)
				}
			}
		})
	}
}
