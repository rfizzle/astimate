package layout

import (
	"encoding/json"
	"math"
	"os"
	"slices"
	"testing"
)

// moduleData is the 61-package module the report mockup was drawn
// from.
const moduleData = "../../../testdata/module-61.json"

// moduleValues returns the bubble values of moduleData, as the
// report sizes them: max(tokens, 400), in path order.
func moduleValues(t *testing.T) []float64 {
	t.Helper()
	raw, err := os.ReadFile(moduleData)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Packages []struct {
			Path   string  `json:"path"`
			Tokens float64 `json:"tokens"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	vs := make([]float64, 0, len(data.Packages))
	for _, p := range data.Packages {
		vs = append(vs, math.Max(p.Tokens, 400))
	}
	return vs
}

// checkPacked fails t when a circle leaves the box, two circles overlap,
// or a circle's area is out of proportion to its value.
func checkPacked(t *testing.T, values []float64, cs []Circle, w, h float64) {
	t.Helper()
	const eps = 1e-6
	if len(cs) != len(values) {
		t.Fatalf("got %d circles for %d values", len(cs), len(values))
	}
	for i, c := range cs {
		if c.X-c.R < -eps || c.X+c.R > w+eps || c.Y-c.R < -eps || c.Y+c.R > h+eps {
			t.Errorf("circle %d %+v leaves the %vx%v box", i, c, w, h)
		}
		for j := i + 1; j < len(cs); j++ {
			if d := math.Hypot(cs[j].X-c.X, cs[j].Y-c.Y); d < c.R+cs[j].R-eps {
				t.Errorf("circles %d and %d overlap: distance %v, radii %v and %v", i, j, d, c.R, cs[j].R)
			}
		}
		if values[0] > 0 && cs[0].R > 0 {
			want := cs[0].R * math.Sqrt(values[i]/values[0])
			if math.Abs(c.R-want) > 1e-6*want+eps {
				t.Errorf("circle %d radius %v, want %v for value %v", i, c.R, want, values[i])
			}
		}
	}
}

func TestPackSixtyone(t *testing.T) {
	t.Parallel()

	values := moduleValues(t)
	if len(values) != 61 {
		t.Fatalf("read %d packages, want 61", len(values))
	}
	cs := Pack(values, 1000, 560, 5)
	checkPacked(t, values, cs, 1000, 560)
	if again := Pack(values, 1000, 560, 5); !slices.Equal(cs, again) {
		t.Error("two packs of the same values differ")
	}
	// The pack fills the box's shorter side: the enclosing circle has
	// radius 280 around the centre, so some circle touches it.
	far := 0.0
	for _, c := range cs {
		far = math.Max(far, math.Hypot(c.X-500, c.Y-280)+c.R)
	}
	if far > 280+1e-6 || far < 270 {
		t.Errorf("outermost edge at %v from the centre, want just under 280", far)
	}
}

// TestPackMatchesD3 checks Pack against the circles d3-hierarchy 3.1.2
// computed for moduleData, as the mockup calls it:
// d3.pack().size([1000, 560]).padding(5) over a hierarchy summed by
// max(tokens, 400), its leaves' [x, y, r] in testdata/d3-pack-61.json.
func TestPackMatchesD3(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/d3-pack-61.json")
	if err != nil {
		t.Fatal(err)
	}
	var want [][3]float64
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	got := Pack(moduleValues(t), 1000, 560, 5)
	if len(got) != len(want) {
		t.Fatalf("got %d circles, want %d", len(got), len(want))
	}
	for i, w := range want {
		c := got[i]
		if math.Abs(c.X-w[0]) > 1e-9 || math.Abs(c.Y-w[1]) > 1e-9 || math.Abs(c.R-w[2]) > 1e-9 {
			t.Errorf("circle %d = %+v, want d3's %v", i, c, w)
		}
	}
}

func TestPackSmall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []float64
	}{
		{"none", nil},
		{"one", []float64{400}},
		{"two", []float64{400, 900}},
		{"three", []float64{400, 400, 400}},
		{"equal", slices.Repeat([]float64{400}, 30)},
		{"mixed", []float64{154081, 400, 400, 52000, 9000, 400, 31000, 400, 1200, 700, 88000, 400}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cs := Pack(tt.values, 1000, 560, 5)
			checkPacked(t, tt.values, cs, 1000, 560)
		})
	}
	// d3.pack pads the root's edge too: radius 20 plus a padding of
	// 5*20/560 on each side, scaled to 280.
	want := Circle{X: 500, Y: 280, R: 280 * 20 / (20 + 2*5*20.0/560)}
	if c := Pack([]float64{400}, 1000, 560, 5)[0]; c.X != want.X || c.Y != want.Y || math.Abs(c.R-want.R) > 1e-9 {
		t.Errorf("one circle = %+v, want %+v", c, want)
	}
}

func TestScale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    Scale
		v    float64
		want float64
	}{
		{"linear middle", Linear(100, 70, 970), 50, 520},
		{"linear end", Linear(100, 70, 970), 100, 970},
		{"sqrt quarter", Sqrt(400, 500, 30), 100, 265},
		{"sqrt zero", Sqrt(400, 500, 30), 0, 500},
		{"empty domain", Linear(0, 70, 970), 0, 520},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.s.At(tt.v); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("At(%v) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}

func TestTicks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		start, stop float64
		count       int
		want        []float64
	}{
		{0, 43.05, 6, []float64{0, 10, 20, 30, 40}},
		{0, 100, 10, []float64{0, 10, 20, 30, 40, 50, 60, 70, 80, 90, 100}},
		{0, 1, 5, []float64{0, 0.2, 0.4, 0.6, 0.8, 1}},
		{0, 1.05, 6, []float64{0, 0.2, 0.4, 0.6, 0.8, 1}},
		{0, 471.8, 5, []float64{0, 100, 200, 300, 400}},
		{3, 3, 6, []float64{3}},
		{0, 1, 0, nil},
	}
	for _, tt := range tests {
		if got := Ticks(tt.start, tt.stop, tt.count); !slices.Equal(got, tt.want) {
			t.Errorf("Ticks(%v, %v, %d) = %v, want %v", tt.start, tt.stop, tt.count, got, tt.want)
		}
	}
}

func TestRing(t *testing.T) {
	t.Parallel()

	circ := 2 * math.Pi * 40
	arcs := Ring([]int{31, 11, 19}, 40, 2)
	if len(arcs) != 3 {
		t.Fatalf("got %d arcs, want 3", len(arcs))
	}
	start := 0.0
	for i, a := range arcs {
		if math.Abs(a.Dash+a.Gap-circ) > 1e-9 || math.Abs(a.Offset+start) > 1e-9 {
			t.Errorf("arc %d = %+v, want dash plus gap %v starting at %v", i, a, circ, start)
		}
		start += a.Dash + 2
	}
	if math.Abs(start-circ) > 1e-9 {
		t.Errorf("arcs cover %v of %v", start, circ)
	}
	if got := Ring([]int{0, 0}, 40, 2); got[0] != (Arc{}) || got[1] != (Arc{}) {
		t.Errorf("Ring of nothing = %+v, want empty arcs", got)
	}
}
