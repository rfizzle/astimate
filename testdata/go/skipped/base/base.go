// Package base is imported by the other two packages.
package base

// Value returns one.
func Value() int { return 1 }

// Counter counts.
type Counter struct{ n int }

// New returns a Counter starting at n. Only the tests of the skipped
// package remote call it.
func New(n int) *Counter { return &Counter{n: n} }

// Add adds d to c. Only the tests of the skipped package remote call it.
func (c *Counter) Add(d int) { c.n += d }

// Checksum hashes s. The skipped package remote holds a copy of it.
func Checksum(s string) int {
	sum := 7
	for i := 0; i < len(s); i++ {
		sum = (sum*31 + int(s[i])) % 1000003
		if sum < 0 {
			sum = -sum
		}
	}
	return sum
}
