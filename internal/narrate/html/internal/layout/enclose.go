package layout

import "math"

// enclose returns the smallest circle enclosing cs, by Welzl's algorithm
// over cs shuffled with rnd, as d3.packEnclose computes it.
func enclose(cs []*Circle, rnd *lcg) Circle {
	order := make([]*Circle, len(cs))
	copy(order, cs)
	for m := len(order); m > 0; {
		i := int(rnd.next() * float64(m))
		m--
		order[m], order[i] = order[i], order[m]
	}
	var basis []*Circle
	var e *Circle
	for i := 0; i < len(order); {
		p := order[i]
		if e != nil && enclosesWeak(e, p) {
			i++
			continue
		}
		basis = extendBasis(basis, p)
		c := encloseBasis(basis)
		e, i = &c, 0
	}
	return *e
}

// extendBasis returns the smallest basis, of B plus p, whose enclosing
// circle holds every circle of B.
func extendBasis(b []*Circle, p *Circle) []*Circle {
	if enclosesWeakAll(p, b) {
		return []*Circle{p}
	}
	for _, x := range b {
		if enclosesNot(p, x) {
			if e := encloseBasis2(x, p); enclosesWeakAll(&e, b) {
				return []*Circle{x, p}
			}
		}
	}
	for i := 0; i < len(b)-1; i++ {
		for j := i + 1; j < len(b); j++ {
			ij, ip, jp := encloseBasis2(b[i], b[j]), encloseBasis2(b[i], p), encloseBasis2(b[j], p)
			if !enclosesNot(&ij, p) || !enclosesNot(&ip, b[j]) || !enclosesNot(&jp, b[i]) {
				continue
			}
			if e := encloseBasis3(b[i], b[j], p); enclosesWeakAll(&e, b) {
				return []*Circle{b[i], b[j], p}
			}
		}
	}
	// Unreachable for circles of finite size; keep p so the loop ends.
	return []*Circle{p}
}

// enclosesNot reports whether a fails to hold b.
func enclosesNot(a, b *Circle) bool {
	dr := a.R - b.R
	dx, dy := b.X-a.X, b.Y-a.Y
	return dr < 0 || dr*dr < dx*dx+dy*dy
}

// enclosesWeak reports whether a holds b, give or take rounding.
func enclosesWeak(a, b *Circle) bool {
	dr := a.R - b.R + math.Max(math.Max(a.R, b.R), 1)*1e-9
	dx, dy := b.X-a.X, b.Y-a.Y
	return dr > 0 && dr*dr > dx*dx+dy*dy
}

// enclosesWeakAll reports whether a holds every circle of b.
func enclosesWeakAll(a *Circle, b []*Circle) bool {
	for _, x := range b {
		if !enclosesWeak(a, x) {
			return false
		}
	}
	return true
}

// encloseBasis returns the circle through the one to three circles of b.
func encloseBasis(b []*Circle) Circle {
	switch len(b) {
	case 1:
		return *b[0]
	case 2:
		return encloseBasis2(b[0], b[1])
	}
	return encloseBasis3(b[0], b[1], b[2])
}

// encloseBasis2 returns the smallest circle holding a and b.
func encloseBasis2(a, b *Circle) Circle {
	x21, y21, r21 := b.X-a.X, b.Y-a.Y, b.R-a.R
	l := math.Sqrt(x21*x21 + y21*y21)
	return Circle{X: (a.X + b.X + x21/l*r21) / 2, Y: (a.Y + b.Y + y21/l*r21) / 2, R: (l + a.R + b.R) / 2}
}

// encloseBasis3 returns the smallest circle tangent to a, b and c from
// outside.
func encloseBasis3(a, b, c *Circle) Circle {
	a2, a3 := a.X-b.X, a.X-c.X
	b2, b3 := a.Y-b.Y, a.Y-c.Y
	c2, c3 := b.R-a.R, c.R-a.R
	d1 := a.X*a.X + a.Y*a.Y - a.R*a.R
	d2 := d1 - b.X*b.X - b.Y*b.Y + b.R*b.R
	d3 := d1 - c.X*c.X - c.Y*c.Y + c.R*c.R
	ab := a3*b2 - a2*b3
	xa := (b2*d3-b3*d2)/(ab*2) - a.X
	xb := (b3*c2 - b2*c3) / ab
	ya := (a3*d2-a2*d3)/(ab*2) - a.Y
	yb := (a2*c3 - a3*c2) / ab
	qa := xb*xb + yb*yb - 1
	qb := 2 * (a.R + xa*xb + ya*yb)
	qc := xa*xa + ya*ya - a.R*a.R
	var r float64
	if math.Abs(qa) > 1e-6 {
		r = -(qb + math.Sqrt(qb*qb-4*qa*qc)) / (2 * qa)
	} else {
		r = -qc / qb
	}
	return Circle{X: a.X + xa + xb*r, Y: a.Y + ya + yb*r, R: r}
}
