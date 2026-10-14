package engine

import (
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
)

// pinnedDefault returns the embedded default configuration with the rules the
// fixtures were written against: min_tokens 40 and no severity: warn lines.
// It pins them so a retune of the shipped default does not move the tests.
func pinnedDefault() []byte {
	var out []string
	for _, l := range strings.Split(string(config.Default()), "\n") {
		switch strings.TrimSpace(l) {
		case "severity: warn":
			continue
		case "min_tokens: 60":
			l = strings.Replace(l, "60", "40", 1)
		}
		out = append(out, l)
	}
	return []byte(strings.Join(out, "\n"))
}

// pinnedConfig parses pinnedDefault.
func pinnedConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Parse(pinnedDefault())
	if err != nil {
		t.Fatalf("parsing the pinned default config: %v", err)
	}
	return cfg
}
