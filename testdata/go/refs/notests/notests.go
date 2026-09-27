// Package notests has three exported funcs and no test files.
package notests

// One returns 1.
func One() int { return 1 }

// Two returns 2.
func Two() int { return 2 }

// Three returns 3.
func Three() int { return one() + Two() }

func one() int { return One() }
