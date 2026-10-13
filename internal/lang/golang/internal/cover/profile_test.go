package cover

import (
	"strings"
	"testing"
)

// sample is a profile of two files of package m/a, one of m/b, and a
// file of m/a/sub, which is another package.
const sample = `mode: set
m/a/a.go:3.20,5.2 2 1
m/a/a.go:7.20,9.2 2 0
m/a/b.go:3.14,4.10 1 0
m/a/b.go:4.10,6.3 3 0

m/b/b.go:3.14,5.2 4 1
m/a/sub/s.go:1.1,2.2 5 1
`

func TestParseProfile(t *testing.T) {
	t.Parallel()

	p, err := ParseProfile([]byte(sample))
	if err != nil {
		t.Fatalf("ParseProfile: %v", err)
	}
	if got := p.Pct("m/a"); got == nil || *got != 25 {
		t.Errorf("Pct(m/a) = %v, want 25 (2 of 8 statements; m/a/sub left out)", got)
	}
	if got := p.Pct("m/b"); got == nil || *got != 100 {
		t.Errorf("Pct(m/b) = %v, want 100", got)
	}
	if got := p.Pct("m/c"); got != nil {
		t.Errorf("Pct(m/c) = %v, want nil for a package the profile does not have", *got)
	}
	for pkg, want := range map[string]bool{"m/a": true, "m/b": true, "m/a/sub": true, "m/c": false, "m": false} {
		if got := p.Has(pkg); got != want {
			t.Errorf("Has(%s) = %v, want %v", pkg, got, want)
		}
	}
}

func TestProfileRan(t *testing.T) {
	t.Parallel()

	p, err := ParseProfile([]byte(sample))
	if err != nil {
		t.Fatalf("ParseProfile: %v", err)
	}
	tests := []struct {
		name     string
		file     string
		from, to Pos
		want     bool
	}{
		{"body holding a run block", "m/a/a.go", Pos{3, 19}, Pos{5, 2}, true},
		{"body holding only blocks that did not run", "m/a/a.go", Pos{7, 19}, Pos{9, 2}, false},
		{"range ending inside the block", "m/a/a.go", Pos{3, 19}, Pos{5, 1}, false},
		{"range starting inside the block", "m/a/a.go", Pos{3, 21}, Pos{5, 2}, false},
		{"whole file", "m/a/a.go", Pos{1, 1}, Pos{100, 1}, true},
		{"other file", "m/a/b.go", Pos{1, 1}, Pos{100, 1}, false},
		{"unknown file", "m/a/x.go", Pos{1, 1}, Pos{100, 1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := p.Ran(tt.file, tt.from, tt.to); got != tt.want {
				t.Errorf("Ran(%s, %v, %v) = %v, want %v", tt.file, tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestProfileMerge(t *testing.T) {
	t.Parallel()

	a, err := ParseProfile([]byte("mode: set\nm/a/a.go:3.20,5.2 2 0\nm/a/a.go:7.20,9.2 2 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseProfile([]byte("mode: set\nm/a/a.go:3.20,5.2 2 1\nm/b/b.go:1.1,2.2 1 0\n"))
	if err != nil {
		t.Fatal(err)
	}
	var merged Profile
	merged.Merge(a)
	merged.Merge(b)
	if !merged.Ran("m/a/a.go", Pos{3, 19}, Pos{5, 2}) {
		t.Error("a block one profile ran is not run in the merge")
	}
	if got := merged.Pct("m/a"); got == nil || *got != 100 {
		t.Errorf("Pct(m/a) = %v, want 100: a shared block is counted once", got)
	}
	if !merged.Has("m/b") {
		t.Error("merge lost m/b")
	}
	var empty Profile
	if empty.Has("m/a") || empty.Pct("m/a") != nil || empty.Ran("m/a/a.go", Pos{1, 1}, Pos{9, 9}) {
		t.Error("the zero Profile is not empty")
	}
}

func TestParseProfileRounding(t *testing.T) {
	t.Parallel()

	// 1 of 3 statements: go test prints 33.3%.
	p, err := ParseProfile([]byte("mode: count\nm/a/a.go:1.1,2.2 1 4\nm/a/a.go:3.1,4.2 2 0\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Pct("m/a"); got == nil || *got != 33.3 {
		t.Errorf("Pct = %v, want 33.3", got)
	}
}

func TestParseProfileMalformed(t *testing.T) {
	t.Parallel()

	for _, line := range []string{
		"no colon here",
		"m/a/a.go:3.20,5.2 2",
		"m/a/a.go:3.20-5.2 2 1",
		"m/a/a.go:3,5.2 2 1",
		"m/a/a.go:3.x,5.2 2 1",
		"m/a/a.go:3.20,5.2 two 1",
	} {
		_, err := ParseProfile([]byte("mode: set\n" + line + "\n"))
		if err == nil || !strings.Contains(err.Error(), "line 2") {
			t.Errorf("ParseProfile(%q) error = %v, want one naming line 2", line, err)
		}
	}
}
