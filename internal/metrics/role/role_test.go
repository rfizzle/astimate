package role_test

import (
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics/role"
)

func TestNames(t *testing.T) {
	want := []string{"main", "library", "test-support", "generated-only"}
	if got := role.Names(); !slices.Equal(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
}

func TestKnown(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{role.Main, true},
		{role.Library, true},
		{role.TestSupport, true},
		{role.GeneratedOnly, true},
		{"", false},
		{"Main", false},
		{"cmd", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := role.Known(tt.in); got != tt.want {
				t.Errorf("Known(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestOf(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", role.Library},
		{role.Main, role.Main},
		{role.TestSupport, role.TestSupport},
	}
	for _, tt := range tests {
		if got := role.Of(tt.in); got != tt.want {
			t.Errorf("Of(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
