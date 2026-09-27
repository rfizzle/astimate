package metrics

import "testing"

func TestModuleWide(t *testing.T) {
	t.Parallel()

	for _, name := range MetricNames() {
		want := name == "dup_blocks_cross_pkg"
		if got := ModuleWide(name); got != want {
			t.Errorf("ModuleWide(%q) = %v, want %v", name, got, want)
		}
	}
}
