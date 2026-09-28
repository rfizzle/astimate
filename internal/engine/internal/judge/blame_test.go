package judge

import (
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// block returns a cross-package block with one occurrence in each of pkgs,
// packages of example.com/m, in the given order.
func block(pkgs ...string) metrics.CrossBlock {
	b := metrics.CrossBlock{Occurrences: make([]metrics.Occurrence, 0, len(pkgs))}
	for i, p := range pkgs {
		b.Occurrences = append(b.Occurrences, metrics.Occurrence{
			Package: "example.com/m/" + p, File: p + "/" + p + ".go", StartLine: 10 * (i + 1), EndLine: 10*(i+1) + 5,
		})
	}
	return b
}

// blockNames renders blocks as their package sets, "a-b", for comparison.
func blockNames(blocks []metrics.CrossBlock) []string {
	out := make([]string, 0, len(blocks))
	for i := range blocks {
		pkgs := blockPackages(&blocks[i])
		for j, p := range pkgs {
			pkgs[j] = Rel("example.com/m", p)
		}
		out = append(out, strings.Join(pkgs, "-"))
	}
	return out
}

func TestBlameCross(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		head, base []metrics.CrossBlock
		known      bool
		named      string
		want       []string
		wantGated  int
	}{
		{
			name:  "copy between two other packages",
			head:  []metrics.CrossBlock{block("a", "b"), block("hidden", "hub")},
			base:  []metrics.CrossBlock{block("a", "b")},
			known: true, named: "trivial", wantGated: 1,
		},
		{
			name:  "new copy touching the named package",
			head:  []metrics.CrossBlock{block("a", "b"), block("hidden", "hub")},
			base:  []metrics.CrossBlock{block("a", "b")},
			known: true, named: "hub", want: []string{"hidden-hub"}, wantGated: 2,
		},
		{
			name:  "old copy touching the named package, new one elsewhere",
			head:  []metrics.CrossBlock{block("a", "hub"), block("b", "c")},
			base:  []metrics.CrossBlock{block("a", "hub")},
			known: true, named: "hub", wantGated: 1,
		},
		{
			name:  "second copy between the same packages is new",
			head:  []metrics.CrossBlock{block("a", "hub"), block("a", "hub")},
			base:  []metrics.CrossBlock{block("a", "hub")},
			known: true, named: "hub", want: []string{"a-hub"}, wantGated: 2,
		},
		{
			name:  "copy removed elsewhere does not hide a new one",
			head:  []metrics.CrossBlock{block("a", "hub")},
			base:  []metrics.CrossBlock{block("b", "c"), block("c", "d")},
			known: true, named: "hub", want: []string{"a-hub"}, wantGated: 3,
		},
		{
			name:  "no block identities, no block touches the named package",
			head:  []metrics.CrossBlock{block("a", "b"), block("hidden", "hub")},
			named: "trivial", wantGated: 1,
		},
		{
			name:  "no block identities, an old block touches the named package",
			head:  []metrics.CrossBlock{block("a", "hub"), block("b", "c")},
			named: "hub", want: []string{"a-hub"}, wantGated: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			named := map[string]bool{"example.com/m/" + tt.named: true}
			b := blameCross(tt.head, tt.base, tt.known, named)
			if got := blockNames(b.blocks); !slices.Equal(got, tt.want) {
				t.Errorf("blamed %v, want %v", got, tt.want)
			}
			// Without identities the baseline file records only a count:
			// the head count before the last copy here.
			baseCount := len(tt.base)
			if !tt.known {
				baseCount = len(tt.head) - 1
			}
			if got := b.gatedCross(len(tt.head), baseCount); got != tt.wantGated {
				t.Errorf("gatedCross = %d, want %d", got, tt.wantGated)
			}
		})
	}
}

// TestNamed checks that blameNamed replaces the row's count with the gated one
// and leaves the rest of the row alone.
func TestBlameNamed(t *testing.T) {
	t.Parallel()

	head := []metrics.CrossBlock{block("a", "b"), block("hidden", "hub")}
	base := []metrics.CrossBlock{block("a", "b")}
	cross, baseCross := 2, 1
	m := metrics.RawMetrics{DupBlocksCrossPkg: &cross}
	bm := &metrics.RawMetrics{DupBlocksCrossPkg: &baseCross}
	for _, tt := range []struct {
		name      string
		named     string
		bm        *metrics.RawMetrics
		want      int
		wantBlame []string
	}{
		{name: "new copy in the named package", named: "hub", bm: bm, want: 2, wantBlame: []string{"hidden-hub"}},
		{name: "copy elsewhere", named: "trivial", bm: bm, want: 1},
		{name: "no baseline row", named: "trivial", want: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, b := blameNamed(m, tt.bm, base, true, head, []string{"example.com/m/" + tt.named})
			if *got.DupBlocksCrossPkg != tt.want {
				t.Errorf("gated dup_blocks_cross_pkg = %d, want %d", *got.DupBlocksCrossPkg, tt.want)
			}
			if names := blockNames(b.blocks); !slices.Equal(names, tt.wantBlame) {
				t.Errorf("blamed %v, want %v", names, tt.wantBlame)
			}
		})
	}
	if *m.DupBlocksCrossPkg != 2 {
		t.Errorf("blameNamed changed its argument's count to %d", *m.DupBlocksCrossPkg)
	}
}

func TestCrossBlameSuggestion(t *testing.T) {
	t.Parallel()

	b := crossBlame{blocks: []metrics.CrossBlock{block("hidden", "hub"), block("a", "b", "hub")}, known: true}
	got := b.suggestion(metrics.RawMetrics{}, "example.com/m")
	const want = "2 duplicate blocks are shared with other packages; extract each into one package, " +
		"starting with the block in hidden/hidden.go:10-15 and hub/hub.go:20-25. " +
		"New blocks touching the checked package are shared by hidden and hub; a, b and hub."
	if got != want {
		t.Errorf("suggestion =\n%q\nwant\n%q", got, want)
	}
	unknown := crossBlame{blocks: []metrics.CrossBlock{block("a", "hub")}}
	const wantUnknown = " Blocks touching the checked package are shared by a and hub."
	if s := unknown.suggestion(metrics.RawMetrics{}, "example.com/m"); !strings.HasSuffix(s, wantUnknown) {
		t.Errorf("suggestion without block identities = %q, want it to end %q", s, wantUnknown)
	}
	if s := (crossBlame{known: true}).suggestion(metrics.RawMetrics{}, "example.com/m"); s != "" {
		t.Errorf("suggestion with no blamed block = %q, want empty", s)
	}
}
