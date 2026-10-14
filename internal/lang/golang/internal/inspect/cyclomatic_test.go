package inspect

import "testing"

// TestComplexityCyclomatic checks each construct gocyclo counts adds
// exactly one to a function's cyclomatic complexity, and that what it does
// not count (default clauses, else, a bare switch or select, other
// operators) adds nothing.
func TestComplexityCyclomatic(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{"empty", "", 1},
		{"if", "if a {\n}", 2},
		{"else adds nothing", "if a {\n} else {\n}", 2},
		{"else if", "if a {\n} else if b {\n}", 3},
		{"for", "for {\n\tbreak\n}", 2},
		{"range", "for range s {\n}", 2},
		{"switch case", "switch x {\ncase 1:\n}", 2},
		{"switch default adds nothing", "switch x {\ncase 1:\ndefault:\n}", 2},
		{"case with two values counts once", "switch x {\ncase 1, 2:\n}", 2},
		{"type switch case", "switch v.(type) {\ncase int:\n}", 2},
		{"select case", "select {\ncase <-c:\n}", 2},
		{"select default adds nothing", "select {\ncase <-c:\ndefault:\n}", 2},
		{"and", "_ = a && b", 2},
		{"or", "_ = a || b", 2},
		{"each operator counts", "_ = a && b || a", 3},
		{"other operators add nothing", "_ = x + 1 == 2", 1},
		{"func literal counts in its parent", "f := func() {\n\tif a {\n\t}\n}\n_ = f", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "package p\nfunc f(a, b bool, x int, s []int, v any, c chan int) {\n" + tt.body + "\n}\n"
			got := Complexity(nil, parseForComplexity(t, src))
			if len(got.PerFunc) != 1 {
				t.Fatalf("perFunc = %+v, want one function", got.PerFunc)
			}
			if c := got.PerFunc[0].Cyclomatic; c != tt.want {
				t.Errorf("cyclomatic = %d, want %d", c, tt.want)
			}
		})
	}
}

// TestComplexityCyclomaticPackage checks the package aggregates: the sum
// over functions, init included, and the nearest-rank p90, and 0 for both
// with no functions.
func TestComplexityCyclomaticPackage(t *testing.T) {
	src := "package p\nfunc init() {}\nfunc f(a bool) {\n\tif a {\n\t}\n}\n" +
		"func g(a, b bool, s []int) {\n\tfor range s {\n\t\tif a && b {\n\t\t}\n\t}\n}\n"
	got := Complexity(nil, parseForComplexity(t, src))
	// init 1, f 2, g 4 (range, if, &&).
	if got.CyclomaticTotal != 7 || got.CyclomaticP90 != 4 {
		t.Errorf("cyclomatic total=%d p90=%d, want 7 and 4", got.CyclomaticTotal, got.CyclomaticP90)
	}
	none := Complexity(nil, parseForComplexity(t, "package p\nconst c = 1\n"))
	if none.CyclomaticTotal != 0 || none.CyclomaticP90 != 0 {
		t.Errorf("no functions: cyclomatic total=%d p90=%d, want 0 and 0", none.CyclomaticTotal, none.CyclomaticP90)
	}
}
