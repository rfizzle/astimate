package gate_test

import (
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// changed returns a changed function named name at head complexity head,
// with baseline complexity base, or no baseline value when base is
// negative.
func changed(name string, head, base int) metrics.ChangedFunction {
	f := metrics.ChangedFunction{FunctionInfo: metrics.FunctionInfo{Name: name, Cognitive: head, File: name + ".go", Line: 3}}
	if base >= 0 {
		f.BaseCognitive, f.HasBase = base, true
	}
	return f
}

// TestEvaluateChangedFunctions checks the per-function verdict of
// changed_func_cognitive_max (SPEC.md 8.1) at the default max 50 and
// over_max_delta 5: a function over the max fails when it is new, crossed
// the max or rose more than over_max_delta, and a legacy function over the
// max that held or rose within it passes.
func TestEvaluateChangedFunctions(t *testing.T) {
	t.Parallel()

	rule := gate.Threshold{Metric: "changed_func_cognitive_max", Kind: gate.Density, Max: ptr(50.0), OverMaxDelta: ptr(5.0)}
	tests := []struct {
		name string
		fns  []metrics.ChangedFunction
		rule gate.Threshold
		// fails names the function the finding judges; empty when the row
		// passes.
		fails     string
		base      float64
		hasBase   bool
		head      float64
		wantLimit string
	}{
		{name: "legacy function unchanged in complexity passes", fns: []metrics.ChangedFunction{changed("Parse", 57, 57)}},
		{name: "legacy function rising within over_max_delta passes", fns: []metrics.ChangedFunction{changed("Parse", 57, 52)}},
		{name: "legacy function falling passes", fns: []metrics.ChangedFunction{changed("Update", 300, 369)}},
		{name: "under the max passes", fns: []metrics.ChangedFunction{changed("small", 50, -1)}},
		{
			name: "legacy function rising past over_max_delta fails", fns: []metrics.ChangedFunction{changed("Update", 385, 369)},
			fails: "Update", base: 369, hasBase: true, head: 385, wantLimit: "over max by 335, allowed 5",
		},
		{
			name: "new function over the max fails", fns: []metrics.ChangedFunction{changed("grade", 60, -1)},
			fails: "grade", head: 60, wantLimit: "max 50",
		},
		{
			name: "crossing the max fails", fns: []metrics.ChangedFunction{changed("Parse", 53, 48)},
			fails: "Parse", base: 48, hasBase: true, head: 53, wantLimit: "max 50",
		},
		{
			name:  "a legacy function staying over the max does not hide a new one",
			fns:   []metrics.ChangedFunction{changed("Update", 369, 369), changed("grade", 60, -1)},
			fails: "grade", head: 60, wantLimit: "max 50",
		},
		{
			name:  "the most complex failing function is named",
			fns:   []metrics.ChangedFunction{changed("a", 55, -1), changed("Update", 400, 369), changed("b", 70, -1)},
			fails: "Update", base: 369, hasBase: true, head: 400, wantLimit: "over max by 350, allowed 5",
		},
		{
			name: "without over_max_delta any rise over the max fails", fns: []metrics.ChangedFunction{changed("Parse", 58, 57)},
			rule:  gate.Threshold{Metric: "changed_func_cognitive_max", Kind: gate.Density, Max: ptr(50.0)},
			fails: "Parse", base: 57, hasBase: true, head: 58, wantLimit: "max 50",
		},
		{
			name: "without over_max_delta a held value passes", fns: []metrics.ChangedFunction{changed("Parse", 57, 57)},
			rule: gate.Threshold{Metric: "changed_func_cognitive_max", Kind: gate.Density, Max: ptr(50.0)},
		},
		{name: "no changed functions passes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := rule
			if tt.rule.Metric != "" {
				r = tt.rule
			}
			worst := 0
			for _, f := range tt.fns {
				worst = max(worst, f.Cognitive)
			}
			head := metrics.RawMetrics{ChangedFuncCognitiveMax: &worst}
			base := &metrics.RawMetrics{}
			got := gate.Evaluate(head, base, []gate.Threshold{r}, nil, gate.WithChangedFunctions(tt.fns))
			if tt.fails == "" {
				if !got.Passed || len(got.Warnings) != 0 {
					t.Fatalf("Evaluate = %+v, want a pass with no findings", got)
				}
				return
			}
			if got.Passed || len(got.Violations) != 1 {
				t.Fatalf("Evaluate = %+v, want one violation", got)
			}
			v := got.Violations[0]
			if v.Function == nil || v.Function.Name != tt.fails {
				t.Errorf("Function = %+v, want %s", v.Function, tt.fails)
			}
			if v.Base != tt.base || v.HasBase != tt.hasBase || v.Head != tt.head || v.Limit != tt.wantLimit {
				t.Errorf("violation = %+v, want base %v (has %v), head %v, limit %q", v, tt.base, tt.hasBase, tt.head, tt.wantLimit)
			}
		})
	}
}

// TestEvaluateChangedFunctionsOptional checks the rule judges the row's
// one value without the option, as before, and that the option leaves
// every other rule alone.
func TestEvaluateChangedFunctionsOptional(t *testing.T) {
	t.Parallel()

	rule := gate.Threshold{Metric: "changed_func_cognitive_max", Kind: gate.Density, Max: ptr(50.0), OverMaxDelta: ptr(5.0)}
	head := metrics.RawMetrics{ChangedFuncCognitiveMax: ptr(57), DupBlocks: 1}
	base := &metrics.RawMetrics{}
	if got := gate.Evaluate(head, base, []gate.Threshold{rule}, nil); got.Passed || got.Violations[0].Function != nil {
		t.Errorf("without the option: %+v, want the row's value to fail with no Function", got)
	}
	fns := []metrics.ChangedFunction{changed("Parse", 57, 57)}
	got := gate.Evaluate(head, base, []gate.Threshold{rule, density("dup_blocks", 0, nil)}, nil, gate.WithChangedFunctions(fns))
	if len(got.Violations) != 1 || got.Violations[0].Metric != "dup_blocks" || got.Violations[0].Function != nil {
		t.Errorf("with the option: %+v, want only the dup_blocks violation", got.Violations)
	}
}

// TestEvaluateChangedFunctionsWarnRule checks a warn rule on the metric
// reports the failing function's breach as a warning that still names it.
func TestEvaluateChangedFunctionsWarnRule(t *testing.T) {
	t.Parallel()

	rule := gate.Threshold{Metric: "changed_func_cognitive_max", Kind: gate.Density, Max: ptr(50.0), Severity: gate.SeverityWarn}
	head := metrics.RawMetrics{ChangedFuncCognitiveMax: ptr(60)}
	got := gate.Evaluate(head, nil, []gate.Threshold{rule}, nil, gate.WithChangedFunctions([]metrics.ChangedFunction{changed("grade", 60, -1)}))
	if !got.Passed || len(got.Warnings) != 1 || got.Warnings[0].Function == nil || got.Warnings[0].Severity != gate.SeverityWarn {
		t.Errorf("Evaluate = %+v, want one warn-severity warning naming grade", got)
	}
}
