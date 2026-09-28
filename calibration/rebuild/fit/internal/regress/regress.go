// Package regress holds the small numerical toolkit the rebuild fit needs:
// ordinary least squares with standard errors, Levenberg-Marquardt for a
// nonlinear least-squares model, rank and linear correlation, and the
// Student t critical value that turns a standard error into a verdict.
// Every routine works on a handful of parameters and at most a few hundred
// observations, so each is written for clarity rather than speed.
package regress

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
)

// SingularError is returned when the normal equations have no unique
// solution: a column is all zero, or a linear combination of the others.
type SingularError struct {
	// Column is the column where elimination found no pivot.
	Column int
	// Zero is true when that column is all zero.
	Zero bool
}

// Error describes the column.
func (e *SingularError) Error() string {
	why := "depends on the others"
	if e.Zero {
		why = "is all zero"
	}
	return "singular normal equations: column " + strconv.Itoa(e.Column) + " " + why
}

// singularTol is the smallest pivot, on the unit-diagonal scaled normal
// matrix, that counts as non-zero.
const singularTol = 1e-10

// Fit is a least-squares solution with its uncertainty.
type Fit struct {
	// Coef are the fitted parameters, in column order.
	Coef []float64
	// SE are their standard errors, from Cov's diagonal.
	SE []float64
	// Cov is the parameters' HC3 covariance matrix (see finish).
	Cov [][]float64
	// Fitted and Residuals are the model's values and y minus them.
	Fitted, Residuals []float64
	// SSE is the residual sum of squares; Sigma2 is SSE / DF.
	SSE, Sigma2 float64
	// DF is the residual degrees of freedom, observations minus
	// parameters.
	DF int
	// R2 and AdjR2 are the coefficient of determination about the mean of
	// y, and its adjustment for the number of parameters.
	R2, AdjR2 float64
}

// T returns the t statistic of parameter j, its value over its standard
// error; 0 when the standard error is 0.
func (f *Fit) T(j int) float64 {
	if f.SE[j] == 0 {
		return 0
	}
	return f.Coef[j] / f.SE[j]
}

// Significant reports whether parameter j differs from zero at about the
// 95% level: |t| is at least the two-sided Student t critical value for
// the fit's degrees of freedom.
func (f *Fit) Significant(j int) bool {
	return f.DF > 0 && math.Abs(f.T(j)) >= TCrit95(f.DF)
}

// RatioSE returns the standard error of Coef[a] / Coef[b] by the delta
// method, using the covariance of the two.
func (f *Fit) RatioSE(a, b int) float64 {
	ca, cb := f.Coef[a], f.Coef[b]
	if cb == 0 {
		return math.Inf(1)
	}
	v := f.Cov[a][a]/(cb*cb) + ca*ca*f.Cov[b][b]/(cb*cb*cb*cb) - 2*ca*f.Cov[a][b]/(cb*cb*cb)
	return math.Sqrt(math.Max(v, 0))
}

// OLS fits y = X b by ordinary least squares. Each row of x is one
// observation; include a column of ones for an intercept. It needs more
// observations than columns and a full-rank x.
func OLS(x [][]float64, y []float64) (Fit, error) {
	n := len(y)
	if n == 0 || len(x) != n {
		return Fit{}, fmt.Errorf("ols: %d rows for %d observations", len(x), n)
	}
	p := len(x[0])
	if n <= p {
		return Fit{}, fmt.Errorf("ols: %d observations for %d parameters", n, p)
	}
	xtx, xty := normal(x, y, nil)
	inv, err := Invert(xtx)
	if err != nil {
		return Fit{}, fmt.Errorf("ols: %w", err)
	}
	coef := mulVec(inv, xty)
	fitted := make([]float64, n)
	for i, row := range x {
		for j, v := range row {
			fitted[i] += v * coef[j]
		}
	}
	return finish(coef, fitted, y, x, inv), nil
}

// normal returns J'J and J'r for Jacobian (or design) jac and vector r. A
// non-nil out is reused for J'J.
func normal(jac [][]float64, r []float64, out [][]float64) (jtj [][]float64, jtr []float64) {
	p := len(jac[0])
	jtj = out
	if jtj == nil {
		jtj = make([][]float64, p)
		for i := range jtj {
			jtj[i] = make([]float64, p)
		}
	}
	jtr = make([]float64, p)
	for i := range p {
		clear(jtj[i])
	}
	for k, row := range jac {
		for i, a := range row {
			jtr[i] += a * r[k]
			for j := i; j < p; j++ {
				jtj[i][j] += a * row[j]
			}
		}
	}
	for i := range p {
		for j := range i {
			jtj[i][j] = jtj[j][i]
		}
	}
	return jtj, jtr
}

// maxLeverage caps an observation's leverage in the HC3 weights, so a
// point the fit passes through exactly does not divide by zero.
const maxLeverage = 0.99

// finish fills a Fit from its parameters, fitted values, observations, the
// design matrix or Jacobian x at the solution, and the inverse of x'x.
// The covariance is the HC3 heteroscedasticity-consistent sandwich
// (x'x)^-1 x' diag(e_i^2 / (1 - h_i)^2) x (x'x)^-1, with h_i the leverage of
// observation i: token counts vary more for larger packages, and the
// plain sigma^2 (x'x)^-1 would then overstate the evidence for any input
// that grows with size.
func finish(coef, fitted, y []float64, x, inv [][]float64) Fit {
	n, p := len(y), len(coef)
	res := make([]float64, n)
	var sse float64
	for i := range y {
		res[i] = y[i] - fitted[i]
		sse += res[i] * res[i]
	}
	df := n - p
	sigma2 := sse / float64(df)
	// meat = x' diag(w) x, w_i = e_i^2 / (1 - h_i)^2, h_i = x_i' inv x_i.
	meat := make([][]float64, p)
	for i := range meat {
		meat[i] = make([]float64, p)
	}
	for k, row := range x {
		var h float64
		for i := range p {
			for j := range p {
				h += row[i] * inv[i][j] * row[j]
			}
		}
		d := 1 - min(h, maxLeverage)
		w := res[k] * res[k] / (d * d)
		for i := range p {
			for j := range p {
				meat[i][j] += w * row[i] * row[j]
			}
		}
	}
	cov := make([][]float64, p)
	se := make([]float64, p)
	for i := range p {
		cov[i] = make([]float64, p)
		for j := range p {
			for a := range p {
				for b := range p {
					cov[i][j] += inv[i][a] * meat[a][b] * inv[b][j]
				}
			}
		}
		se[i] = math.Sqrt(math.Max(cov[i][i], 0))
	}
	r2 := 1 - sse/sst(y)
	return Fit{
		Coef: coef, SE: se, Cov: cov, Fitted: fitted, Residuals: res,
		SSE: sse, Sigma2: sigma2, DF: df,
		R2: r2, AdjR2: 1 - (1-r2)*float64(n-1)/float64(df),
	}
}

// sst is the total sum of squares of y about its mean.
func sst(y []float64) float64 {
	m := Mean(y)
	var s float64
	for _, v := range y {
		s += (v - m) * (v - m)
	}
	return s
}

// Invert returns the inverse of the symmetric positive semi-definite
// matrix a by Gauss-Jordan elimination with partial pivoting, after
// scaling it to a unit diagonal so the pivot test does not depend on the
// columns' units. It returns a *SingularError when a pivot vanishes.
func Invert(a [][]float64) ([][]float64, error) {
	p := len(a)
	scale := make([]float64, p)
	for i := range p {
		if a[i][i] <= 0 {
			return nil, &SingularError{Column: i, Zero: true}
		}
		scale[i] = 1 / math.Sqrt(a[i][i])
	}
	// Augmented [S A S | I].
	m := make([][]float64, p)
	for i := range p {
		m[i] = make([]float64, 2*p)
		for j := range p {
			m[i][j] = a[i][j] * scale[i] * scale[j]
		}
		m[i][p+i] = 1
	}
	for c := range p {
		piv := c
		for r := c + 1; r < p; r++ {
			if math.Abs(m[r][c]) > math.Abs(m[piv][c]) {
				piv = r
			}
		}
		if math.Abs(m[piv][c]) < singularTol {
			return nil, &SingularError{Column: c}
		}
		m[c], m[piv] = m[piv], m[c]
		d := m[c][c]
		for j := range m[c] {
			m[c][j] /= d
		}
		for r := range p {
			if r == c || m[r][c] == 0 {
				continue
			}
			f := m[r][c]
			for j := range m[r] {
				m[r][j] -= f * m[c][j]
			}
		}
	}
	inv := make([][]float64, p)
	for i := range p {
		inv[i] = make([]float64, p)
		for j := range p {
			inv[i][j] = m[i][p+j] * scale[i] * scale[j]
		}
	}
	return inv, nil
}

// Model evaluates a nonlinear model at parameters theta: its value at every
// observation and the Jacobian, one row of partial derivatives per
// observation.
type Model func(theta []float64) (f []float64, jac [][]float64)

// lmMaxIter bounds the Levenberg-Marquardt iterations; lmTol is the
// relative change in SSE below which it stops.
const (
	lmMaxIter = 500
	lmTol     = 1e-12
)

// LM fits model to y by nonlinear least squares with the
// Levenberg-Marquardt method, starting from theta0, and returns the
// solution with standard errors from the Jacobian at it. It fails when
// there are no more observations than parameters or the Jacobian at the
// solution is rank-deficient.
func LM(model Model, y, theta0 []float64) (Fit, error) {
	n, p := len(y), len(theta0)
	if n <= p {
		return Fit{}, fmt.Errorf("lm: %d observations for %d parameters", n, p)
	}
	theta := slices.Clone(theta0)
	f, jac := model(theta)
	sse := sumSq(y, f)
	lambda := 1e-3
	jtj := make([][]float64, p)
	for i := range jtj {
		jtj[i] = make([]float64, p)
	}
	for range lmMaxIter {
		jtj, jtr := normal(jac, diff(y, f), jtj)
		improved := false
		for lambda < 1e16 {
			step, err := damped(jtj, jtr, lambda)
			if err != nil {
				lambda *= 10
				continue
			}
			trial := make([]float64, p)
			for i := range p {
				trial[i] = theta[i] + step[i]
			}
			tf, tj := model(trial)
			if tsse := sumSq(y, tf); tsse < sse && !math.IsNaN(tsse) {
				done := (sse - tsse) <= lmTol*sse
				theta, f, jac, sse = trial, tf, tj, tsse
				lambda = math.Max(lambda/10, 1e-12)
				improved = !done
				break
			}
			lambda *= 10
		}
		if !improved {
			break
		}
	}
	jtj, _ = normal(jac, diff(y, f), jtj)
	inv, err := Invert(jtj)
	if err != nil {
		return Fit{}, fmt.Errorf("lm: at the solution: %w", err)
	}
	return finish(theta, f, y, jac, inv), nil
}

// damped solves (J'J + lambda diag(J'J)) step = J'r.
func damped(jtj [][]float64, jtr []float64, lambda float64) ([]float64, error) {
	p := len(jtr)
	a := make([][]float64, p)
	for i := range p {
		a[i] = slices.Clone(jtj[i])
		a[i][i] += lambda * math.Max(jtj[i][i], 1e-30)
	}
	inv, err := Invert(a)
	if err != nil {
		return nil, err
	}
	return mulVec(inv, jtr), nil
}

// mulVec returns the product of the square matrix m and v.
func mulVec(m [][]float64, v []float64) []float64 {
	out := make([]float64, len(v))
	for i, row := range m {
		for j, a := range row {
			out[i] += a * v[j]
		}
	}
	return out
}

// diff returns y - f.
func diff(y, f []float64) []float64 {
	d := make([]float64, len(y))
	for i := range y {
		d[i] = y[i] - f[i]
	}
	return d
}

// sumSq returns the sum of squares of y - f.
func sumSq(y, f []float64) float64 {
	var s float64
	for i := range y {
		d := y[i] - f[i]
		s += d * d
	}
	return s
}

// Mean returns the arithmetic mean of xs; 0 for none.
func Mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, v := range xs {
		s += v
	}
	return s / float64(len(xs))
}

// Median returns the median of xs, the mean of the middle two for an even
// count; 0 for none. xs is not modified.
func Median(xs []float64) float64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// Pearson returns the linear correlation of x and y, and false when either
// is constant or there are fewer than three pairs.
func Pearson(x, y []float64) (float64, bool) {
	n := len(x)
	if n < 3 || len(y) != n {
		return 0, false
	}
	mx, my := Mean(x), Mean(y)
	var sxy, sxx, syy float64
	for i := range x {
		dx, dy := x[i]-mx, y[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx == 0 || syy == 0 {
		return 0, false
	}
	return sxy / math.Sqrt(sxx*syy), true
}

// Spearman returns the rank correlation of x and y (Pearson on ranks, ties
// sharing their mean rank), and false as Pearson does.
func Spearman(x, y []float64) (float64, bool) {
	return Pearson(Ranks(x), Ranks(y))
}

// Ranks returns the 1-based ranks of xs, tied values sharing the mean of
// the ranks they span.
func Ranks(xs []float64) []float64 {
	idx := make([]int, len(xs))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return xs[idx[a]] < xs[idx[b]] })
	r := make([]float64, len(xs))
	for i := 0; i < len(idx); {
		j := i
		for j+1 < len(idx) && xs[idx[j+1]] == xs[idx[i]] {
			j++
		}
		mean := float64(i+j)/2 + 1
		for k := i; k <= j; k++ {
			r[idx[k]] = mean
		}
		i = j + 1
	}
	return r
}

// CorrSignificant reports whether a correlation r over n pairs differs
// from zero at about the 95% level, by the t test on n-2 degrees of
// freedom.
func CorrSignificant(r float64, n int) bool {
	if n < 3 {
		return false
	}
	if math.Abs(r) >= 1 {
		return true
	}
	t := math.Abs(r) * math.Sqrt(float64(n-2)/(1-r*r))
	return t >= TCrit95(n-2)
}

// TCrit95 returns the two-sided 95% critical value of Student's t with df
// degrees of freedom: tabulated to four decimals up to df 10, and the
// Cornish-Fisher expansion about the normal quantile beyond, which is
// within 0.05% there.
func TCrit95(df int) float64 {
	if df <= 0 {
		return math.Inf(1)
	}
	if df <= 10 {
		return [...]float64{12.7062, 4.3027, 3.1824, 2.7764, 2.5706, 2.4469, 2.3646, 2.3060, 2.2622, 2.2281}[df-1]
	}
	const z = 1.959963984540054
	d := float64(df)
	z3, z5, z7 := z*z*z, z*z*z*z*z, z*z*z*z*z*z*z
	return z + (z3+z)/(4*d) + (5*z5+16*z3+3*z)/(96*d*d) +
		(3*z7+19*z5+17*z3-15*z)/(384*d*d*d)
}
