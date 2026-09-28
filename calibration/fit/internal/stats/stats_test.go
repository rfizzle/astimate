package stats

import "testing"

func TestPercentile(t *testing.T) {
	series := make([]float64, 100)
	for i := range series {
		series[i] = float64(i + 1)
	}
	tests := []struct {
		name   string
		values []float64
		p      float64
		want   float64
	}{
		{"1..100 p25", series, 25, 25},
		{"1..100 p50", series, 50, 50},
		{"1..100 p75", series, 75, 75},
		{"1..100 p90", series, 90, 90},
		{"1..100 p95", series, 95, 95},
		{"1..100 p0", series, 0, 1},
		{"1..100 p100", series, 100, 100},
		{"four values p50", []float64{1, 2, 3, 4}, 50, 2},
		{"four values p90", []float64{1, 2, 3, 4}, 90, 4},
		{"single", []float64{7}, 90, 7},
		{"empty", nil, 90, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := percentile(tt.values, tt.p); got != tt.want {
				t.Errorf("percentile(p%v) = %v, want %v", tt.p, got, tt.want)
			}
		})
	}
}

func TestComputeStats(t *testing.T) {
	values := make([]float64, 100)
	for i := range values {
		// Reversed, so ComputeStats must sort.
		values[i] = float64(100 - i)
	}
	s := ComputeStats(values)
	if s.N != 100 || s.Min != 1 || s.Max != 100 || s.P50 != 50 || s.P90 != 90 || s.IQR != 50 {
		t.Errorf("stats = %+v, want n 100, min 1, max 100, p50 50, p90 90, IQR 50", s)
	}
	if len(s.Hist) != histBins {
		t.Fatalf("histogram has %d bins, want %d", len(s.Hist), histBins)
	}
	total := 0
	for _, b := range s.Hist {
		total += b.Count
	}
	if total != 100 {
		t.Errorf("histogram counts %d values, want 100", total)
	}
	if last := s.Hist[histBins-1]; !last.Open || last.Count != 5 || last.Lo != 95 {
		t.Errorf("open bin = %+v, want the 5 values above p95 95", last)
	}
	if first := s.Hist[0]; first.Count != 11 {
		// Bins over [1, 95] are 94/9 wide: 1..11 fall in the first.
		t.Errorf("first bin = %+v, want 11 values", first)
	}
}

func TestHistogramConstant(t *testing.T) {
	s := ComputeStats([]float64{3, 3, 3})
	if s.Hist[0].Count != 3 || s.Hist[histBins-1].Count != 0 {
		t.Errorf("constant series histogram = %+v, want every value in the first bin", s.Hist)
	}
}

func TestRoundReadable(t *testing.T) {
	tests := []struct {
		v       float64
		percent bool
		want    float64
	}{
		{0, false, 0},
		{0.26, true, 0.5},
		{0.3, false, 0},
		{6, false, 6},
		{7.3, false, 7},
		{7.3, true, 7.5},
		{9.74, true, 9.5},
		{10, false, 10},
		{12.5, false, 15}, // 13 at two significant figures
		{16, false, 15},
		{23, false, 25},
		{48.7, true, 50},
		{100, false, 100},
		{101, false, 100},
		{874, false, 850}, // 870 at two significant figures
		{899, false, 900},
		{1000, false, 1000},
		{1234, false, 1000},
		{1250, false, 1500}, // 1300 at two significant figures
		{2325, false, 2500},
		{30864, false, 31000},
		{789448, false, 790000},
	}
	for _, tt := range tests {
		if got := RoundReadable(tt.v, tt.percent); got != tt.want {
			t.Errorf("RoundReadable(%v, percent %t) = %v, want %v", tt.v, tt.percent, got, tt.want)
		}
	}
}

func TestDeltaFromIQR(t *testing.T) {
	tests := []struct {
		iqr     float64
		percent bool
		want    float64
	}{
		{0, false, 1},
		{0, true, 0.5},
		{2, false, 1},
		{2, true, 0.5},
		{3, false, 1},
		{8, false, 2},
		{9, false, 3},
		{11, false, 3},
		{29.5, true, 7.5},
		{30, true, 7.5},
		{30.1, true, 8},
	}
	for _, tt := range tests {
		if got := DeltaFromIQR(tt.iqr, tt.percent); got != tt.want {
			t.Errorf("DeltaFromIQR(%v, percent %t) = %v, want %v", tt.iqr, tt.percent, got, tt.want)
		}
	}
}

func TestIsPercent(t *testing.T) {
	if !IsPercent("duplication_pct") {
		t.Error("duplication_pct should be a percent metric")
	}
	if IsPercent("sloc") {
		t.Error("sloc should not be a percent metric")
	}
}

func TestMinStep(t *testing.T) {
	if got := MinStep(true); got != 0.5 {
		t.Errorf("MinStep(true) = %v, want 0.5", got)
	}
	if got := MinStep(false); got != 1 {
		t.Errorf("MinStep(false) = %v, want 1", got)
	}
}
