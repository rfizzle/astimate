package exclude

import (
	"strings"
	"testing"
)

func TestListMatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		{"star in segment", "internal/*.go", "internal/a.go", true},
		{"star stays in its segment", "internal/*.go", "internal/sub/a.go", false},
		{"star at root only", "*.go", "pkg/a.go", false},
		{"question mark", "a?.go", "ab.go", true},
		{"question mark one rune", "a?.go", "abc.go", false},
		{"class", "gen/[ab].go", "gen/b.go", true},
		{"class miss", "gen/[ab].go", "gen/c.go", false},
		{"negated class", "gen/[^ab].go", "gen/c.go", true},
		{"double star at start", "**/*_gen.go", "x_gen.go", true},
		{"double star at start deep", "**/*_gen.go", "a/b/c/x_gen.go", true},
		{"double star at start miss", "**/*_gen.go", "a/b/x.go", false},
		{"double star in middle none", "web/**/generated", "web/generated/a.ts", true},
		{"double star in middle many", "web/**/generated", "web/src/api/generated/a.ts", true},
		{"double star in middle miss", "web/**/generated", "api/generated/a.ts", false},
		{"double star at end", "internal/legacy/**", "internal/legacy/a/b.go", true},
		{"double star at end direct", "internal/legacy/**", "internal/legacy/b.go", true},
		{"double star at end not a sibling", "internal/legacy/**", "internal/legacyx/b.go", false},
		{"lone double star", "**", "a/b.go", true},
		{"directory pattern", "internal/gen", "internal/gen/a.go", true},
		{"directory pattern deep", "internal/gen", "internal/gen/sub/a.go", true},
		{"directory pattern trailing slash", "internal/gen/", "internal/gen/a.go", true},
		{"directory pattern prefix only", "internal/gen", "internal/generated/a.go", false},
		{"exact file", "cmd/main.go", "cmd/main.go", true},
		{"outside the root", "**", "../a.go", false},
		{"cleaned path", "a/b.go", "./a/b.go", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l, err := New([]string{tt.pattern})
			if err != nil {
				t.Fatal(err)
			}
			_, pat, got := l.Match(tt.path)
			if got != tt.want {
				t.Fatalf("Match(%q) with %q = %v, want %v", tt.path, tt.pattern, got, tt.want)
			}
			if got && pat != tt.pattern {
				t.Errorf("Match(%q) pattern = %q, want %q", tt.path, pat, tt.pattern)
			}
		})
	}
}

func TestListFirstPattern(t *testing.T) {
	t.Parallel()
	l, err := New([]string{"a/x.go", "a/**", "**/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"a/x.go": "a/x.go", "a/y.go": "a/**", "b/y.go": "**/*.go"} {
		if _, got, ok := l.Match(path); !ok || got != want {
			t.Errorf("Match(%q) = %q, %v, want %q", path, got, ok, want)
		}
	}
	if i, _, _ := l.Match("b/y.go"); i != 2 {
		t.Errorf("Match(b/y.go) index = %d, want 2", i)
	}
	if i, _, ok := l.Match("b/y.txt"); ok || i != -1 {
		t.Errorf("Match(b/y.txt) = %d, %v, want -1, false", i, ok)
	}
	if got := l.Patterns(); len(got) != 3 {
		t.Errorf("Patterns() = %v, want three", got)
	}
}

func TestListNil(t *testing.T) {
	t.Parallel()
	l, err := New(nil)
	if err != nil || l != nil {
		t.Fatalf("New(nil) = %v, %v, want nil, nil", l, err)
	}
	if l.Matches("a.go") {
		t.Error("a nil list matched")
	}
	if l.Patterns() != nil {
		t.Error("a nil list has patterns")
	}
}

func TestListMalformed(t *testing.T) {
	t.Parallel()
	for _, p := range []string{"", " ", "/abs/x", "a//b", "a/../b", "./a", "a/b**", "**x/a", "a/[b", "a/[]"} {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			_, err := New([]string{"ok/**", p})
			if err == nil {
				t.Fatalf("New(%q) = nil error, want one", p)
			}
			if !strings.Contains(err.Error(), "exclude[1]") || !strings.Contains(err.Error(), `"`+p+`"`) {
				t.Errorf("error %q does not name exclude[1] %q", err, p)
			}
		})
	}
}

func TestReason(t *testing.T) {
	t.Parallel()
	if got, want := Reason("internal/gen/**"), "excluded by config (internal/gen/**)"; got != want {
		t.Errorf("ExcludedReason = %q, want %q", got, want)
	}
}
