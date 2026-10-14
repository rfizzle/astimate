package gate_test

import (
	"reflect"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// duplicatedLinesRule is the duplicated_lines rule SPEC.md 8.1 describes:
// density, no new duplicated line, judged past floor.
func duplicatedLinesRule(floor float64) gate.Threshold {
	r := density("duplicated_lines", 0, nil)
	r.Floor = ptr(floor)
	return r
}

// TestEvaluateDuplicatedLines checks a duplicated_lines rule beside the
// default rules: it fails a copy that merged two duplicate blocks into one
// longer one while dup_blocks and duplication_pct fell (kubesentry's
// internal/output, 2026-10-13), never fails a value at or below its floor,
// a fall or a package new at head, and its finding merges with the other
// duplication rules' findings into one.
func TestEvaluateDuplicatedLines(t *testing.T) {
	t.Parallel()

	rules := append(defaultRules(t), duplicatedLinesRule(10))
	dup := func(blocks int, pct float64, lines int) func(*metrics.RawMetrics) {
		return func(m *metrics.RawMetrics) {
			m.DupBlocks, m.DuplicationPct, m.DuplicatedLines = blocks, pct, ptr(lines)
		}
	}
	runFloorCases(t, rules, []floorCase{
		{
			name: "a third copy that merged two blocks fails while dup_blocks and duplication_pct fell",
			base: dup(6, 83.3, 135),
			head: dup(5, 73.7, 157),
			want: []gate.Violation{{Metric: "duplicated_lines", Base: 135, Head: 157, HasBase: true, Limit: "max_delta +0"}},
		},
		{
			name: "a rise to the floor passes",
			base: dup(1, 0.5, 3),
			head: dup(1, 1.6, 10),
		},
		{
			name: "a rise past the floor fails",
			base: dup(1, 1.6, 10),
			head: dup(1, 1.8, 11),
			want: []gate.Violation{{Metric: "duplicated_lines", Base: 10, Head: 11, HasBase: true, Limit: "max_delta +0"}},
		},
		{
			name: "unchanged lines pass",
			base: dup(4, 20, 120),
			head: dup(4, 19, 120),
		},
		{
			name: "fewer lines pass",
			base: dup(4, 20, 120),
			head: dup(3, 15, 90),
		},
		{
			name:   "a package new at head is not judged without ratchet_from_zero",
			noBase: true,
			head:   dup(0, 30, 200),
		},
		{
			name: "a paste is one finding on dup_blocks carrying duplicated_lines and duplication_pct",
			base: dup(1, 2.5, 15),
			head: dup(3, 12.25, 74),
			want: []gate.Violation{{Metric: "dup_blocks", Base: 1, Head: 3, HasBase: true,
				Limit: "max_delta +0; duplication_pct 2.5 -> 12.25, max_delta +6; duplicated_lines 15 -> 74, max_delta +0",
				Also:  []string{"duplication_pct", "duplicated_lines"}}},
		},
		{
			name: "without dup_blocks, duplication_pct merges into duplicated_lines",
			base: dup(2, 4, 24),
			head: dup(2, 11, 66),
			want: []gate.Violation{{Metric: "duplicated_lines", Base: 24, Head: 66, HasBase: true,
				Limit: "max_delta +0; duplication_pct 4 -> 11, max_delta +6", Also: []string{"duplication_pct"}}},
		},
	})
}

// TestEvaluateDuplicatedLinesFloors checks the floors the rule was swept
// at (calibration/notes/duplicated-lines-rule-2026-10-14.md): a rise of
// one line from 15 fails at floors 0 and 10 and passes at 20.
func TestEvaluateDuplicatedLinesFloors(t *testing.T) {
	t.Parallel()

	base := metrics.RawMetrics{DuplicatedLines: ptr(15)}
	head := metrics.RawMetrics{DuplicatedLines: ptr(16)}
	for floor, fails := range map[float64]bool{0: true, 10: true, 20: false} {
		got := gate.Evaluate(head, &base, []gate.Threshold{duplicatedLinesRule(floor)}, nil)
		if got.Passed == fails {
			t.Errorf("floor %v: Passed = %t, want %t", floor, got.Passed, !fails)
		}
	}
}

// TestEvaluateDuplicatedLinesNotComputed checks that a baseline without
// duplicated_lines, a baseline file written before it existed, skips the
// rule's max_delta with a note rather than failing.
func TestEvaluateDuplicatedLinesNotComputed(t *testing.T) {
	t.Parallel()

	head := metrics.RawMetrics{DuplicatedLines: ptr(40)}
	got := gate.Evaluate(head, &metrics.RawMetrics{}, []gate.Threshold{duplicatedLinesRule(10)}, nil)
	want := gate.Result{Passed: true, Notes: []gate.Note{{Metric: "duplicated_lines", Text: "not computed at baseline; max_delta skipped"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Evaluate() = %+v, want %+v", got, want)
	}
}
