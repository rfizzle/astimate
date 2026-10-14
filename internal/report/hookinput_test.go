package report

import (
	"io"
	"strings"
	"testing"
)

func TestStopHookActive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input io.Reader
		want  bool
	}{
		{"true", strings.NewReader(`{"hook_event_name":"Stop","stop_hook_active":true}`), true},
		{"false", strings.NewReader(`{"hook_event_name":"Stop","stop_hook_active":false}`), false},
		{"field absent", strings.NewReader(`{"hook_event_name":"Stop"}`), false},
		{"no input", nil, false},
		{"empty input", strings.NewReader(""), false},
		{"malformed", strings.NewReader(`{"stop_hook_active": tru`), false},
		{"wrong type", strings.NewReader(`{"stop_hook_active":"true"}`), false},
		{"oversized", io.MultiReader(strings.NewReader(`{"stop_hook_active":true,"pad":"`),
			strings.NewReader(strings.Repeat("x", MaxHookInput)), strings.NewReader(`"}`)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := StopHookActive(tt.input); got != tt.want {
				t.Errorf("StopHookActive = %v, want %v", got, tt.want)
			}
		})
	}
}
