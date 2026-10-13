package metrics

import (
	"slices"
	"testing"
)

func TestUntestedRulesClone(t *testing.T) {
	r := UntestedRules{OptionFuncs: []string{"WithX"}, InterfaceMethods: []string{"t.Emit"}, TestSupport: []string{"F.Do"}}
	c := r.Clone()
	c.OptionFuncs[0] = "changed"
	if r.OptionFuncs[0] != "WithX" || !slices.Equal(c.InterfaceMethods, r.InterfaceMethods) || !slices.Equal(c.TestSupport, r.TestSupport) {
		t.Errorf("Clone shares storage or drops a list: %+v from %+v", c, r)
	}
}
