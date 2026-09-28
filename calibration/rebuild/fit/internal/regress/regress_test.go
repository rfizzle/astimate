package regress_test

import (
	"errors"
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/regress"
)

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol*math.Max(1, math.Abs(b)) }

func TestOLS(t *testing.T) {
	// y = 3 + 2 x1 - 0.5 x2, exactly.
	var x [][]float64
	var y []float64
	for i := range 20 {
		x1, x2 := float64(i), float64((i*7)%11)
		x = append(x, []float64{1, x1, x2})
		y = append(y, 3+2*x1-0.5*x2)
	}
	f, err := regress.OLS(x, y)
	if err != nil {
		t.Fatal(err)
	}
	for j, want := range []float64{3, 2, -0.5} {
		if !near(f.Coef[j], want, 1e-9) {
			t.Errorf("coef[%d] = %v, want %v", j, f.Coef[j], want)
		}
	}
	if !near(f.R2, 1, 1e-12) || f.DF != 17 || f.SSE > 1e-12 {
		t.Errorf("R2 %v DF %d SSE %v, want an exact fit with 17 degrees of freedom", f.R2, f.DF, f.SSE)
	}

	tests := []struct {
		name string
		x    [][]float64
		y    []float64
	}{
		{"too few", [][]float64{{1, 2}, {1, 3}}, []float64{1, 2}},
		{"collinear", [][]float64{{1, 2, 4}, {1, 3, 6}, {1, 4, 8}, {1, 5, 10}}, []float64{1, 2, 3, 4}},
		{"zero column", [][]float64{{1, 0}, {1, 0}, {1, 0}}, []float64{1, 2, 3}},
		{"shape", [][]float64{{1}}, []float64{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := regress.OLS(tt.x, tt.y); err == nil {
				t.Error("no error")
			}
		})
	}
	if _, err := regress.OLS([][]float64{{1, 2, 4}, {1, 3, 6}, {1, 4, 8}, {1, 5, 10}}, []float64{1, 2, 3, 4}); !errors.As(err, new(*regress.SingularError)) {
		t.Errorf("collinear: %v, want ErrSingular", err)
	}
}

// TestOLSStandardErrors checks the HC3 standard errors against the spread
// of the slope over repeated noisy samples, with noise that grows with x.
func TestOLSStandardErrors(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	const trials = 400
	covered := 0
	var slopes []float64
	for range trials {
		var x [][]float64
		var y []float64
		for i := range 40 {
			v := float64(i + 1)
			x = append(x, []float64{1, v})
			y = append(y, 10+3*v+0.1*v*v*rng.NormFloat64())
		}
		f, err := regress.OLS(x, y)
		if err != nil {
			t.Fatal(err)
		}
		slopes = append(slopes, f.Coef[1])
		if math.Abs(f.Coef[1]-3) <= regress.TCrit95(f.DF)*f.SE[1] {
			covered++
		}
		if f.Significant(1) != (math.Abs(f.T(1)) >= regress.TCrit95(f.DF)) {
			t.Fatal("Significant disagrees with T")
		}
	}
	if covered < trials*90/100 {
		t.Errorf("95%% intervals covered the slope in %d of %d samples", covered, trials)
	}
	if m := regress.Mean(slopes); !near(m, 3, 0.02) {
		t.Errorf("mean slope %v, want 3", m)
	}
}

func TestRatioSE(t *testing.T) {
	f := regress.Fit{Coef: []float64{2, 4}, Cov: [][]float64{{0.04, 0}, {0, 0.16}}, SE: []float64{0.2, 0.4}}
	// Var(a/b) = Var(a)/b^2 + a^2 Var(b)/b^4 = 0.0025 + 0.0025.
	if got := f.RatioSE(0, 1); !near(got, math.Sqrt(0.005), 1e-12) {
		t.Errorf("RatioSE = %v, want %v", got, math.Sqrt(0.005))
	}
	f.Coef[1] = 0
	if !math.IsInf(f.RatioSE(0, 1), 1) {
		t.Error("ratio over zero has a finite SE")
	}
	f.SE[0] = 0
	if f.T(0) != 0 {
		t.Error("T with zero SE is not 0")
	}
}

func TestInvert(t *testing.T) {
	a := [][]float64{{4, 1, 2}, {1, 3, 0}, {2, 0, 5}}
	inv, err := regress.Invert(a)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		for j := range 3 {
			var s float64
			for k := range 3 {
				s += a[i][k] * inv[k][j]
			}
			want := 0.0
			if i == j {
				want = 1
			}
			if !near(s, want, 1e-12) {
				t.Errorf("(A inv)[%d][%d] = %v, want %v", i, j, s, want)
			}
		}
	}
	var zero *regress.SingularError
	if _, err := regress.Invert([][]float64{{0, 0}, {0, 1}}); !errors.As(err, &zero) || !zero.Zero || zero.Error() != "singular normal equations: column 0 is all zero" {
		t.Errorf("zero column: %v", err)
	}
	if _, err := regress.Invert([][]float64{{1, 1}, {1, 1}}); !errors.As(err, new(*regress.SingularError)) || !strings.Contains(err.Error(), "column 1 depends on the others") {
		t.Errorf("singular: %v", err)
	}
}

func TestLM(t *testing.T) {
	// y = a * exp(b x), from a start far from a = 2, b = 0.3.
	var xs, y []float64
	for i := range 30 {
		x := float64(i) / 3
		xs = append(xs, x)
		y = append(y, 2*math.Exp(0.3*x))
	}
	model := func(theta []float64) ([]float64, [][]float64) {
		f := make([]float64, len(xs))
		jac := make([][]float64, len(xs))
		for i, x := range xs {
			e := math.Exp(theta[1] * x)
			f[i] = theta[0] * e
			jac[i] = []float64{e, theta[0] * x * e}
		}
		return f, jac
	}
	fit, err := regress.LM(model, y, []float64{10, -0.5})
	if err != nil {
		t.Fatal(err)
	}
	if !near(fit.Coef[0], 2, 1e-6) || !near(fit.Coef[1], 0.3, 1e-6) {
		t.Errorf("LM = %v, want [2 0.3]", fit.Coef)
	}
	if _, err := regress.LM(model, y[:2], []float64{1, 1}); err == nil {
		t.Error("two observations for two parameters fitted")
	}
	flat := func(theta []float64) ([]float64, [][]float64) {
		f := make([]float64, len(y))
		jac := make([][]float64, len(y))
		for i := range y {
			f[i] = theta[0]
			jac[i] = []float64{1, 0}
		}
		return f, jac
	}
	if _, err := regress.LM(flat, y, []float64{1, 1}); err == nil {
		t.Error("a parameter the model ignores was fitted")
	}
}

func TestCorrelation(t *testing.T) {
	x := []float64{1, 2, 3, 4, 5}
	tests := []struct {
		name         string
		y            []float64
		pearson, rho float64
		ok           bool
	}{
		{"linear", []float64{2, 4, 6, 8, 10}, 1, 1, true},
		{"monotone", []float64{1, 8, 27, 64, 125}, 0.9431, 1, true},
		{"reversed", []float64{5, 4, 3, 2, 1}, -1, -1, true},
		{"constant", []float64{1, 1, 1, 1, 1}, 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := regress.Pearson(x, tt.y)
			r, _ := regress.Spearman(x, tt.y)
			if ok != tt.ok || !near(p, tt.pearson, 1e-4) || !near(r, tt.rho, 1e-12) {
				t.Errorf("Pearson %v %v, Spearman %v; want %v %v, %v", p, ok, r, tt.pearson, tt.ok, tt.rho)
			}
		})
	}
	if _, ok := regress.Pearson(x[:2], x[:2]); ok {
		t.Error("correlation of two points")
	}
	if got := regress.Ranks([]float64{10, 20, 10, 5}); got[0] != 2.5 || got[1] != 4 || got[2] != 2.5 || got[3] != 1 {
		t.Errorf("Ranks = %v, want [2.5 4 2.5 1]", got)
	}
	for _, tt := range []struct {
		r   float64
		n   int
		sig bool
	}{{0.5, 30, true}, {0.3, 30, false}, {0.99, 3, false}, {1, 3, true}, {0.5, 2, false}} {
		if got := regress.CorrSignificant(tt.r, tt.n); got != tt.sig {
			t.Errorf("CorrSignificant(%v, %d) = %v, want %v", tt.r, tt.n, got, tt.sig)
		}
	}
}

func TestSummaries(t *testing.T) {
	if got := regress.Median([]float64{3, 1, 2}); got != 2 {
		t.Errorf("median of three = %v", got)
	}
	if got := regress.Median([]float64{4, 1, 3, 2}); got != 2.5 {
		t.Errorf("median of four = %v", got)
	}
	if regress.Median(nil) != 0 || regress.Mean(nil) != 0 {
		t.Error("empty summaries are not 0")
	}
	if got := regress.Mean([]float64{1, 2, 6}); got != 3 {
		t.Errorf("mean = %v", got)
	}
}

func TestTCrit95(t *testing.T) {
	for _, tt := range []struct {
		df   int
		want float64
	}{{1, 12.7062}, {2, 4.3027}, {3, 3.1824}, {5, 2.5706}, {10, 2.2281}, {11, 2.2010}, {15, 2.1314}, {30, 2.0423}, {1000, 1.9623}} {
		if got := regress.TCrit95(tt.df); math.Abs(got-tt.want) > 5e-4*tt.want {
			t.Errorf("TCrit95(%d) = %v, want %v", tt.df, got, tt.want)
		}
	}
	if !math.IsInf(regress.TCrit95(0), 1) {
		t.Error("TCrit95(0) is finite")
	}
}
