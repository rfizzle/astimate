// Package layout computes the geometry of the HTML report's charts in Go,
// so the page needs no charting library: the bubble map's circle packing,
// the scatter's scales and ticks, and the band ring's arcs. Every function
// is deterministic: the same input gives the same numbers.
package layout

import "math"

// Circle is a circle placed by Pack, its centre and radius in the box.
type Circle struct {
	X, Y, R float64
}

// node is one circle on the front chain of packSiblings.
type node struct {
	c          *Circle
	next, prev *node
}

// lcg is d3's linear congruential generator, the source of the shuffle
// in enclose, seeded as d3.pack seeds it.
type lcg struct{ s uint64 }

// next returns the next number in [0, 1).
func (g *lcg) next() float64 {
	const a, c, m = 1664525, 1013904223, 1 << 32
	g.s = (a*g.s + c) % m
	return float64(g.s) / m
}

// Pack places one circle per value, of area proportional to the value,
// inside a width by height box with padding between neighbours, the way
// d3.pack lays out the leaves of a one-level hierarchy: the front-chain
// method of Wang et al. places the circles in input order, the smallest
// enclosing circle (Welzl's algorithm over a shuffled order) bounds them,
// and the result is scaled to fit the box's shorter side and centred. The
// circles come back in input order.
func Pack(values []float64, width, height, padding float64) []Circle {
	cs := make([]Circle, len(values))
	if len(values) == 0 {
		return cs
	}
	for i, v := range values {
		cs[i].R = math.Sqrt(math.Max(0, v))
	}
	rnd := &lcg{s: 1}
	side := math.Min(width, height)
	rootR := packSiblings(cs, rnd)
	if pad := padding * rootR / side; pad > 0 {
		for i := range cs {
			cs[i].R += pad
		}
		rootR = packSiblings(cs, rnd) + pad
		for i := range cs {
			cs[i].R -= pad
		}
	}
	k := 1.0
	if rootR > 0 {
		k = side / (2 * rootR)
	}
	for i := range cs {
		cs[i].X = width/2 + k*cs[i].X
		cs[i].Y = height/2 + k*cs[i].Y
		cs[i].R *= k
	}
	return cs
}

// packSiblings places cs tangent to one another around the origin with
// the front-chain method and returns the radius of their enclosing
// circle, which it centres on the origin.
func packSiblings(cs []Circle, rnd *lcg) float64 {
	n := len(cs)
	a := &cs[0]
	a.X, a.Y = 0, 0
	if n == 1 {
		return a.R
	}
	b := &cs[1]
	a.X, b.X, b.Y = -b.R, a.R, 0
	if n == 2 {
		return a.R + b.R
	}
	place(b, a, &cs[2])
	na, nb, nc := &node{c: a}, &node{c: b}, &node{c: &cs[2]}
	na.next, nc.prev = nb, nb
	nb.next, na.prev = nc, nc
	nc.next, nb.prev = na, na
	for i := 3; i < n; i++ {
		na, nb = placeOnChain(na, nb, &cs[i])
	}
	chain := []*Circle{nb.c}
	for c := nb.next; c != nb; c = c.next {
		chain = append(chain, c.c)
	}
	e := enclose(chain, rnd)
	for i := range cs {
		cs[i].X -= e.X
		cs[i].Y -= e.Y
	}
	return e.R
}

// placeOnChain places c tangent to a and b, stepping a or b along the
// chain past every circle c would overlap, inserts c between them and
// returns the new pair closest to the centroid.
func placeOnChain(a, b *node, c *Circle) (na, nb *node) {
	for {
		place(a.c, b.c, c)
		j, k := b.next, a.prev
		sj, sk := b.c.R, a.c.R
		moved := false
		for {
			if sj <= sk {
				if intersects(j.c, c) {
					b = j
					a.next, b.prev = b, a
					moved = true
					break
				}
				sj, j = sj+j.c.R, j.next
			} else {
				if intersects(k.c, c) {
					a = k
					a.next, b.prev = b, a
					moved = true
					break
				}
				sk, k = sk+k.c.R, k.prev
			}
			if j == k.next {
				break
			}
		}
		if !moved {
			break
		}
	}
	nc := &node{c: c, prev: a, next: b}
	a.next, b.prev = nc, nc
	best, bestScore := a, score(a)
	for x := nc.next; x != nc; x = x.next {
		if s := score(x); s < bestScore {
			best, bestScore = x, s
		}
	}
	return best, best.next
}

// place puts c tangent to both a and b.
func place(b, a, c *Circle) {
	dx, dy := b.X-a.X, b.Y-a.Y
	d2 := dx*dx + dy*dy
	if d2 == 0 {
		c.X, c.Y = a.X+c.R, a.Y
		return
	}
	a2, b2 := (a.R+c.R)*(a.R+c.R), (b.R+c.R)*(b.R+c.R)
	if a2 > b2 {
		x := (d2 + b2 - a2) / (2 * d2)
		y := math.Sqrt(math.Max(0, b2/d2-x*x))
		c.X, c.Y = b.X-x*dx-y*dy, b.Y-x*dy+y*dx
		return
	}
	x := (d2 + a2 - b2) / (2 * d2)
	y := math.Sqrt(math.Max(0, a2/d2-x*x))
	c.X, c.Y = a.X+x*dx-y*dy, a.Y+x*dy+y*dx
}

// intersects reports whether a and b overlap by more than rounding.
func intersects(a, b *Circle) bool {
	dr := a.R + b.R - 1e-6
	dx, dy := b.X-a.X, b.Y-a.Y
	return dr > 0 && dr*dr > dx*dx+dy*dy
}

// score is the squared distance from the origin to the weighted midpoint
// of a and the circle after it.
func score(n *node) float64 {
	a, b := n.c, n.next.c
	ab := a.R + b.R
	dx := (a.X*b.R + b.X*a.R) / ab
	dy := (a.Y*b.R + b.Y*a.R) / ab
	return dx*dx + dy*dy
}
