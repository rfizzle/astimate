package gate_test

import (
	"reflect"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// floorCase is one row of the floor and direction tests: base and head
// built from healthy (head from fresh for a new package), and the
// violations the rules report.
type floorCase struct {
	name       string
	base, head func(*metrics.RawMetrics)
	// noBase evaluates head as a package new at head.
	noBase bool
	want   []gate.Violation
}

// runFloorCases evaluates each case under rules and compares the
// violations.
func runFloorCases(t *testing.T, rules []gate.Threshold, tests []floorCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			head := healthy()
			if tt.noBase {
				head = fresh()
			}
			if tt.head != nil {
				tt.head(&head)
			}
			var base *metrics.RawMetrics
			if !tt.noBase {
				b := healthy()
				if tt.base != nil {
					tt.base(&b)
				}
				base = &b
			}
			got := gate.Evaluate(head, base, rules, nil)
			if len(got.Violations) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got.Violations, tt.want) {
				t.Errorf("Evaluate().Violations =\n%+v\nwant\n%+v", got.Violations, tt.want)
			}
		})
	}
}

// TestEvaluateFloor checks the density floors of the default rules
// (SPEC.md 8.1, 8.2): a rise to a value at or below the floor passes, and
// one past it is judged by max_delta and max as before.
func TestEvaluateFloor(t *testing.T) {
	t.Parallel()

	runFloorCases(t, defaultRules(t), []floorCase{
		{
			name: "max_nesting 1 to 2 passes under the floor of 4",
			base: func(m *metrics.RawMetrics) { m.MaxNesting = 1 },
			head: func(m *metrics.RawMetrics) { m.MaxNesting = 2 },
		},
		{
			name: "max_nesting 3 to 4 passes at the floor",
			base: func(m *metrics.RawMetrics) { m.MaxNesting = 3 },
			head: func(m *metrics.RawMetrics) { m.MaxNesting = 4 },
		},
		{
			name: "max_nesting 4 to 5 fails past the floor",
			base: func(m *metrics.RawMetrics) { m.MaxNesting = 4 },
			head: func(m *metrics.RawMetrics) { m.MaxNesting = 5 },
			want: []gate.Violation{{Metric: "max_nesting", Base: 4, Head: 5, HasBase: true, Limit: "max_delta +0"}},
		},
		{
			name: "cognitive_p90 1 to 8 with a changed function at 8 passes under the floor of 10",
			base: func(m *metrics.RawMetrics) { m.CognitiveP90 = 1 },
			head: func(m *metrics.RawMetrics) { m.CognitiveP90, m.ChangedFuncCognitiveMax = 8, ptr(8) },
		},
		{
			name: "cognitive_p90 2 to 11 with a changed function at 11 fails past the floor",
			base: func(m *metrics.RawMetrics) { m.CognitiveP90 = 2 },
			head: func(m *metrics.RawMetrics) { m.CognitiveP90, m.ChangedFuncCognitiveMax = 11, ptr(11) },
			want: []gate.Violation{{Metric: "cognitive_p90", Base: 2, Head: 11, HasBase: true, Limit: "max_delta +3"}},
		},
		{
			name: "duplication_pct 1 to 10 with more duplicated lines passes at the floor of 10",
			base: func(m *metrics.RawMetrics) { m.DuplicationPct, m.DuplicatedLines = 1, ptr(6) },
			head: func(m *metrics.RawMetrics) { m.DuplicationPct, m.DuplicatedLines = 10, ptr(60) },
		},
		{
			name:   "a new package is judged by the max only",
			noBase: true,
			head:   func(m *metrics.RawMetrics) { m.MaxNesting = 6 },
			want:   []gate.Violation{{Metric: "max_nesting", Head: 6, Limit: "max 5"}},
		},
	})

	t.Run("floor holds a ratchet_from_zero rule on a new package", func(t *testing.T) {
		t.Parallel()

		rule := gate.Threshold{Metric: "globals", Kind: gate.Density, MaxDelta: ptr(0.0), RatchetFromZero: true, Floor: ptr(1.0)}
		head := metrics.RawMetrics{Globals: 1}
		if got := gate.Evaluate(head, nil, []gate.Threshold{rule}, nil); !got.Passed {
			t.Errorf("globals 1 under floor 1 on a new package: %+v, want a pass", got.Violations)
		}
		head.Globals = 2
		if got := gate.Evaluate(head, nil, []gate.Threshold{rule}, nil); got.Passed {
			t.Error("globals 2 over floor 1 on a new package passed, want max_delta +0 from zero")
		}
	})
}

// TestEvaluateDuplicationNumerator checks that duplication_pct is judged
// only when duplicated_lines rose (SPEC.md 8.1): a share that rises
// because code was deleted passes.
func TestEvaluateDuplicationNumerator(t *testing.T) {
	t.Parallel()

	// go-starter's postgres package: the same six blocks while sloc fell
	// from 406 to 320 took duplication_pct from 26.8 to 34.1.
	postgres := func(sloc, blocks, lines int) func(*metrics.RawMetrics) {
		return func(m *metrics.RawMetrics) {
			m.SLOC, m.DupBlocks, m.DuplicatedLines = sloc, blocks, ptr(lines)
			m.DuplicationPct = float64(int(float64(lines)/float64(sloc)*1000+0.5)) / 10
		}
	}
	runFloorCases(t, defaultRules(t), []floorCase{
		{
			name: "a rise only because sloc fell passes",
			base: postgres(406, 6, 109),
			head: postgres(320, 6, 109),
		},
		{
			name: "a rise with fewer duplicated lines passes",
			base: postgres(406, 6, 109),
			head: postgres(250, 5, 100),
		},
		{
			name: "the same sloc and one more block fails",
			base: postgres(406, 6, 109),
			head: postgres(406, 7, 140),
			want: []gate.Violation{{
				Metric: "dup_blocks", Base: 6, Head: 7, HasBase: true,
				Limit: "max_delta +0; duplication_pct 26.8 -> 34.5, max_delta +6", Also: []string{"duplication_pct"},
			}},
		},
		{
			name: "a baseline without duplicated_lines is judged by the share as before",
			base: func(m *metrics.RawMetrics) { m.SLOC, m.DupBlocks, m.DuplicationPct = 406, 6, 26.8 },
			head: postgres(320, 6, 109),
			want: []gate.Violation{{Metric: "duplication_pct", Base: 26.8, Head: 34.1, HasBase: true, Limit: "max_delta +6"}},
		},
	})

	t.Run("the rule alone fails a rise of the duplicated lines", func(t *testing.T) {
		t.Parallel()

		rule := density("duplication_pct", 6, ptr(40.0))
		base := metrics.RawMetrics{SLOC: 400, DuplicationPct: 20, DuplicatedLines: ptr(80)}
		head := metrics.RawMetrics{SLOC: 400, DuplicationPct: 30, DuplicatedLines: ptr(120)}
		if got := gate.Evaluate(head, &base, []gate.Threshold{rule}, nil); got.Passed {
			t.Error("duplication_pct 20 -> 30 with duplicated lines 80 -> 120 passed, want max_delta +6")
		}
	})

	t.Run("crossing the max only because sloc fell passes", func(t *testing.T) {
		t.Parallel()

		rule := density("duplication_pct", 6, ptr(40.0))
		base := metrics.RawMetrics{SLOC: 400, DuplicationPct: 38, DuplicatedLines: ptr(152)}
		head := metrics.RawMetrics{SLOC: 360, DuplicationPct: 42.2, DuplicatedLines: ptr(152)}
		if got := gate.Evaluate(head, &base, []gate.Threshold{rule}, nil); !got.Passed {
			t.Errorf("duplication_pct 38 -> 42.2 with the same duplicated lines: %+v, want a pass", got.Violations)
		}
	})
}

// TestRiseCondition checks the direction test is described for the two
// metrics rose judges, and for no other.
func TestRiseCondition(t *testing.T) {
	t.Parallel()

	for metric, want := range map[string]bool{"duplication_pct": true, "cognitive_p90": true, "max_nesting": false, "dup_blocks": false} {
		if cond, ok := gate.RiseCondition(metric); ok != want || (cond != "") != want {
			t.Errorf("RiseCondition(%q) = %q, %v; want a condition: %v", metric, cond, ok, want)
		}
	}
}

// TestEvaluateCognitiveP90ChangedFunctions checks that cognitive_p90 is
// judged only when a function changed since the baseline is more complex
// than the baseline's p90 (SPEC.md 8.1): a p90 that rises because simple
// functions were deleted passes.
func TestEvaluateCognitiveP90ChangedFunctions(t *testing.T) {
	t.Parallel()

	p90 := func(v int, changed *int) func(*metrics.RawMetrics) {
		return func(m *metrics.RawMetrics) { m.CognitiveP90, m.ChangedFuncCognitiveMax = v, changed }
	}
	runFloorCases(t, defaultRules(t), []floorCase{
		{
			// go-starter's server: fifteen trivial WithX funcs deleted.
			name: "a rise from deleting simple functions passes",
			base: p90(3, nil),
			head: p90(11, ptr(0)),
		},
		{
			name: "a rise with changed functions no more complex than the baseline p90 passes",
			base: p90(3, nil),
			head: p90(11, ptr(3)),
		},
		{
			name: "a changed function above the baseline p90 fails",
			base: p90(3, nil),
			head: p90(11, ptr(12)),
			want: []gate.Violation{{Metric: "cognitive_p90", Base: 3, Head: 11, HasBase: true, Limit: "max_delta +3"}},
		},
		{
			name: "crossing the max only because functions were deleted passes",
			base: p90(19, nil),
			head: p90(22, ptr(5)),
		},
		{
			name: "without a function-level diff it is judged as before",
			base: p90(3, nil),
			head: p90(11, nil),
			want: []gate.Violation{{Metric: "cognitive_p90", Base: 3, Head: 11, HasBase: true, Limit: "max_delta +3"}},
		},
		{
			name:   "a new package is judged by the max",
			noBase: true,
			head:   p90(22, ptr(30)),
			want:   []gate.Violation{{Metric: "cognitive_p90", Head: 22, Limit: "max 20"}},
		},
	})
}
