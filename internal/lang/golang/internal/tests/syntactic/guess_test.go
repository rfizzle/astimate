package syntactic

import (
	"slices"
	"testing"
)

// TestGuessNames checks the names guessed for an unnamed import outside
// the module.
func TestGuessNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want []string
	}{
		{path: "fmt", want: []string{"fmt"}},
		{path: "math/rand/v2", want: []string{"rand"}},
		{path: "gopkg.in/yaml.v3", want: []string{"yaml"}},
		{path: "github.com/x/go-foo", want: []string{"go-foo", "foo"}},
		{path: "github.com/x/bar-go", want: []string{"bar-go", "bar"}},
		{path: "v2", want: []string{"v2"}},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			if got := guessNames(tc.path); !slices.Equal(got, tc.want) {
				t.Errorf("guessNames(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
