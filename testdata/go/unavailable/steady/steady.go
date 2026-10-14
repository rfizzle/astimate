// Package steady loads at every commit and never changes, so the baseline
// is a module that loads except for one package.
package steady

// One returns one.
func One() int { return 1 }
