package metrics

import (
	"strconv"
	"testing"
)

// optString renders an optional value exactly, for comparison and messages.
func optString(p *float64) string {
	if p == nil {
		return "null"
	}
	return strconv.FormatFloat(*p, 'g', -1, 64)
}

func TestInstability(t *testing.T) {
	tests := []struct {
		name           string
		fanIn, imports int
		want           *float64
	}{
		{name: "hub: fan-in 4, fan-out 0", fanIn: 4, imports: 0, want: ptr(0.0)},
		{name: "leaf consumer: fan-in 0, fan-out 1", fanIn: 0, imports: 1, want: ptr(1.0)},
		{name: "balanced", fanIn: 3, imports: 1, want: ptr(0.25)},
		{name: "no internal edges", fanIn: 0, imports: 0, want: nil},
		{name: "negative total", fanIn: -1, imports: 0, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Instability(tt.fanIn, tt.imports)
			if optString(got) != optString(tt.want) {
				t.Errorf("Instability(%d, %d) = %s, want %s", tt.fanIn, tt.imports, optString(got), optString(tt.want))
			}
		})
	}
}

func TestAbstractness(t *testing.T) {
	tests := []struct {
		name              string
		interfaces, types int
		want              *float64
	}{
		{name: "only exported type is an interface", interfaces: 1, types: 1, want: ptr(1.0)},
		{name: "all concrete", interfaces: 0, types: 3, want: ptr(0.0)},
		{name: "mixed", interfaces: 1, types: 4, want: ptr(0.25)},
		{name: "no exported types", interfaces: 0, types: 0, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Abstractness(tt.interfaces, tt.types)
			if optString(got) != optString(tt.want) {
				t.Errorf("Abstractness(%d, %d) = %s, want %s", tt.interfaces, tt.types, optString(got), optString(tt.want))
			}
		})
	}
}

func TestMainSequenceDistance(t *testing.T) {
	tests := []struct {
		name                      string
		abstractness, instability *float64
		want                      *float64
	}{
		{name: "stable concrete leaf", abstractness: ptr(0.0), instability: ptr(0.0), want: ptr(1.0)},
		{name: "on the main sequence", abstractness: ptr(0.25), instability: ptr(0.75), want: ptr(0.0)},
		{name: "abstract and unstable", abstractness: ptr(1.0), instability: ptr(1.0), want: ptr(1.0)},
		{name: "below the line", abstractness: ptr(0.5), instability: ptr(0.25), want: ptr(0.25)},
		{name: "abstractness null", abstractness: nil, instability: ptr(1.0), want: nil},
		{name: "instability null", abstractness: ptr(1.0), instability: nil, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MainSequenceDistance(tt.abstractness, tt.instability)
			if optString(got) != optString(tt.want) {
				t.Errorf("MainSequenceDistance = %s, want %s", optString(got), optString(tt.want))
			}
		})
	}
}

func TestRoundRatio(t *testing.T) {
	tests := []struct {
		name string
		v    *float64
		want *float64
	}{
		{name: "noise below, as |0.2 + 1 - 1|", v: ptr(0.19999999999999996), want: ptr(0.2)},
		{name: "noise above, as 0.1 + 0.2", v: ptr(0.30000000000000004), want: ptr(0.3)},
		{name: "noise around zero", v: ptr(1.1e-16), want: ptr(0.0)},
		{name: "one third", v: ptr(1.0 / 3), want: ptr(0.333)},
		{name: "half rounds away from zero", v: ptr(0.0625), want: ptr(0.063)},
		{name: "exact", v: ptr(1.0), want: ptr(1.0)},
		{name: "nil", v: nil, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RoundRatio(tt.v); optString(got) != optString(tt.want) {
				t.Errorf("RoundRatio(%s) = %s, want %s", optString(tt.v), optString(got), optString(tt.want))
			}
		})
	}
}
