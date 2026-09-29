package metrics

import (
	"slices"
	"strings"
	"testing"
)

// TestFieldTableMatchesStruct checks each fieldTable row against the
// RawMetrics field slots puts at its index: a v1 row is a pointer, a row
// with an upper bound is a float and only a float has one, and the flags
// are the known ones.
func TestFieldTableMatchesStruct(t *testing.T) {
	t.Parallel()

	var zero RawMetrics
	full := fullMetrics()
	table, zs, fs := fieldTable(), zero.slots(), full.slots()
	for i := range table {
		f := table[i]
		t.Run(f.name, func(t *testing.T) {
			_, zeroOK, _ := read(zs[i])
			_, fullOK, k := read(fs[i])
			if !fullOK {
				t.Fatalf("slot %d of fullMetrics reads as not computed", i)
			}
			if got, want := zeroOK, f.release == "v0"; got != want {
				t.Errorf("release %s, but the zero record's slot computed = %v", f.release, got)
			}
			if got, want := k == kindFloat, f.upper > 0; got != want {
				t.Errorf("upper %v, but the slot is a float: %v", f.upper, got)
			}
			for w := range strings.FieldsSeq(f.flags) {
				if !slices.Contains([]string{"gated", "capacity", "module-wide"}, w) {
					t.Errorf("unknown flag %q", w)
				}
			}
		})
	}
	if _, ok, _ := read(42); ok {
		t.Error("read of a non-slot reported ok")
	}
}
