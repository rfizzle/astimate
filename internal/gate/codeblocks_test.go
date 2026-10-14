package gate_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
)

// codeRules returns the default rules, every one at severity fail, with
// the dup_blocks rule judging dup_blocks_code in its place, the form a
// configuration that gates code blocks only takes (SPEC.md 8.2).
func codeRules(t *testing.T) []gate.Threshold {
	t.Helper()
	rules := defaultRules(t)
	i := slices.IndexFunc(rules, func(r gate.Threshold) bool { return r.Metric == "dup_blocks" })
	if i < 0 {
		t.Fatal("the default has no dup_blocks rule")
	}
	rules[i].Metric = "dup_blocks_code"
	return rules
}

// TestEvaluateBlockKinds adds one duplicate block of each kind (SPEC.md
// 6.3) to a package and checks which rule fails it: the default's
// dup_blocks rule judges every kind, and a dup_blocks_code rule in its
// place fails only the code block, a declarative, idiom or self-overlap
// block raising dup_blocks alone.
func TestEvaluateBlockKinds(t *testing.T) {
	t.Parallel()

	base := healthy()
	base.DupBlocksCode = ptr(1)
	for _, tc := range []struct {
		kind string
		code bool
	}{
		{"code", true},
		{"declarative", false},
		{"idiom", false},
		{"self-overlap", false},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			head := base
			head.DupBlocks++
			head.DupBlocksCode = ptr(1)
			if tc.code {
				head.DupBlocksCode = ptr(2)
			}
			if got := failed(gate.Evaluate(head, &base, defaultRules(t), nil)); !reflect.DeepEqual(got, []string{"dup_blocks"}) {
				t.Errorf("default rules fail %v, want [dup_blocks]", got)
			}
			var want []string
			if tc.code {
				want = []string{"dup_blocks_code"}
			}
			if got := failed(gate.Evaluate(head, &base, codeRules(t), nil)); !reflect.DeepEqual(got, want) {
				t.Errorf("dup_blocks_code rule fails %v, want %v", got, want)
			}
		})
	}
}

// TestEvaluateCodeBlocksNew checks a package new at head under a
// dup_blocks_code rule: counted from zero, so one code block fails and
// blocks of the other kinds alone pass.
func TestEvaluateCodeBlocksNew(t *testing.T) {
	t.Parallel()

	head := fresh()
	head.DupBlocks, head.DupBlocksCode = 2, ptr(0)
	if got := failed(gate.Evaluate(head, nil, codeRules(t), nil)); got != nil {
		t.Errorf("two blocks, no code block, fail %v, want none", got)
	}
	head.DupBlocksCode = ptr(1)
	if got := failed(gate.Evaluate(head, nil, codeRules(t), nil)); !reflect.DeepEqual(got, []string{"dup_blocks_code"}) {
		t.Errorf("one code block fails %v, want [dup_blocks_code]", got)
	}
}

// TestEvaluateCodeBlocksMerge checks the merge table's dup_blocks_code
// entries: under a dup_blocks_code rule it carries duplication_pct as
// dup_blocks would, and with both rules gated it merges into dup_blocks.
// A baseline that predates dup_blocks_code skips the delta with a note.
func TestEvaluateCodeBlocksMerge(t *testing.T) {
	t.Parallel()

	base := healthy()
	base.DupBlocks, base.DupBlocksCode, base.DuplicationPct = 1, ptr(1), 2.5
	head := base
	head.DupBlocks, head.DupBlocksCode, head.DuplicationPct = 3, ptr(2), 12.25

	got := gate.Evaluate(head, &base, codeRules(t), nil).Violations
	want := []gate.Violation{{Metric: "dup_blocks_code", Base: 1, Head: 2, HasBase: true,
		Limit: "max_delta +0; duplication_pct 2.5 -> 12.25, max_delta +6", Also: []string{"duplication_pct"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dup_blocks_code rule: %+v, want %+v", got, want)
	}

	code := codeRules(t)
	i := slices.IndexFunc(code, func(r gate.Threshold) bool { return r.Metric == "dup_blocks_code" })
	both := append(defaultRules(t), code[i])
	got = gate.Evaluate(head, &base, both, nil).Violations
	want = []gate.Violation{{Metric: "dup_blocks", Base: 1, Head: 3, HasBase: true,
		Limit: "max_delta +0; dup_blocks_code 1 -> 2, max_delta +0; duplication_pct 2.5 -> 12.25, max_delta +6",
		Also:  []string{"dup_blocks_code", "duplication_pct"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("both rules: %+v, want %+v", got, want)
	}

	old := base
	old.DupBlocksCode = nil
	res := gate.Evaluate(head, &old, codeRules(t), nil)
	if v := failed(res); slices.Contains(v, "dup_blocks_code") {
		t.Errorf("a baseline without dup_blocks_code fails %v", v)
	}
	if !slices.ContainsFunc(res.Notes, func(n gate.Note) bool { return n.Metric == "dup_blocks_code" }) {
		t.Errorf("notes %+v, want one on dup_blocks_code", res.Notes)
	}
}

// failed returns the metrics of res's violations, nil for none.
func failed(res gate.Result) []string {
	var out []string
	for _, v := range res.Violations {
		out = append(out, v.Metric)
	}
	return out
}
