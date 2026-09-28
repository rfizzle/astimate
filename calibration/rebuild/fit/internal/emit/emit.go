// Package emit writes the fitted configuration: the base file with its
// config_version and the rebuild section's fitted parameters replaced and
// every other byte kept, so the change to the default is a diff of a few
// lines. The result is parsed, validated and checked to be calibrated
// before it is returned.
package emit

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/score"
)

// Config returns base with config_version set to version and the rebuild
// section's context_budget, tokens_per_export, tokens_per_untested_export,
// tokens_per_hidden_state and superlinear_exponent set from p. Each key
// must appear exactly once where it is replaced; a comment after a value
// is kept. The result must parse, carry exactly p's values in those keys,
// and be calibrated (score.Calibrated).
func Config(base []byte, version string, p score.RebuildParams) ([]byte, error) {
	if !score.Calibrated(version) {
		return nil, fmt.Errorf("config_version %q is not a rebuild calibration", version)
	}
	lines := strings.SplitAfter(string(base), "\n")
	if err := replaceKey(lines, "", "config_version", version); err != nil {
		return nil, err
	}
	start, end, err := section(lines, "rebuild")
	if err != nil {
		return nil, err
	}
	for _, kv := range []struct {
		key string
		v   float64
	}{
		{"context_budget", p.ContextBudget},
		{"tokens_per_export", p.TokensPerExport},
		{"tokens_per_untested_export", p.TokensPerUntestedExport},
		{"tokens_per_hidden_state", p.TokensPerHiddenState},
		{"superlinear_exponent", p.SuperlinearExponent},
	} {
		if err := replaceKey(lines[start:end], "  ", kv.key, format(kv.v)); err != nil {
			return nil, fmt.Errorf("rebuild: %w", err)
		}
	}
	out := []byte(strings.Join(lines, ""))
	cfg, err := config.Parse(out)
	if err != nil {
		return nil, fmt.Errorf("fitted config does not validate: %w", err)
	}
	got := cfg.Rebuild
	if got.ContextBudget != p.ContextBudget || got.TokensPerExport != p.TokensPerExport ||
		got.TokensPerUntestedExport != p.TokensPerUntestedExport ||
		got.TokensPerHiddenState != p.TokensPerHiddenState || got.SuperlinearExponent != p.SuperlinearExponent {
		return nil, fmt.Errorf("fitted config reads back as %+v, want %+v", got, p)
	}
	if cfg.Version != version {
		return nil, fmt.Errorf("fitted config reads back config_version %q, want %q", cfg.Version, version)
	}
	return out, nil
}

// format writes v as YAML: an integer without a decimal point, anything
// else in its shortest form.
func format(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// section returns the line range of the top-level mapping key's block: the
// lines after "key:" up to the next line that starts a top-level key.
func section(lines []string, key string) (start, end int, err error) {
	start = -1
	for i, l := range lines {
		t := strings.TrimRight(l, "\r\n")
		if start < 0 {
			if t == key+":" || strings.HasPrefix(t, key+": #") || strings.HasPrefix(t, key+":  #") {
				start = i + 1
			}
			continue
		}
		if t != "" && t[0] != ' ' && t[0] != '#' {
			return start, i, nil
		}
	}
	if start < 0 {
		return 0, 0, fmt.Errorf("base config has no top-level %s section", key)
	}
	return start, len(lines), nil
}

// replaceKey sets the value of the one line in lines that is exactly
// indent + key + ":" at that indentation, keeping any trailing comment. It
// fails when there is no such line or more than one.
func replaceKey(lines []string, indent, key, value string) error {
	prefix := indent + key + ":"
	found := -1
	for i, l := range lines {
		if !strings.HasPrefix(l, prefix) {
			continue
		}
		if found >= 0 {
			return fmt.Errorf("%s appears more than once", key)
		}
		found = i
	}
	if found < 0 {
		return fmt.Errorf("base config has no %q line", prefix)
	}
	l := lines[found]
	rest := strings.TrimRight(l[len(prefix):], "\r\n")
	nl := l[len(prefix)+len(rest):]
	comment := ""
	if i := strings.Index(rest, " #"); i >= 0 {
		comment = rest[i:]
	}
	var b bytes.Buffer
	b.WriteString(prefix)
	b.WriteByte(' ')
	b.WriteString(value)
	b.WriteString(comment)
	b.WriteString(nl)
	lines[found] = b.String()
	return nil
}
