// Package emit writes the fitted YAML output of calibration/fit: a whole
// candidate configuration, or a languages.<id> override block to merge
// into one.
package emit

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/calibration/fit/internal/pool"
	"github.com/rfizzle/astimate/calibration/fit/internal/report"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"gopkg.in/yaml.v3"
)

// Language writes the languages.<lang> override block of the fitted
// choices: one rule per overridden choice, in the base's order, each a
// whole rule (it replaces the top-level rule on its metric for lang) with
// the fitted limits and the base rule's kind, ratchet_from_zero and
// warn_at. No rebuild parameter is set. The block is validated merged
// into the base configuration baseData, and must resolve to version.
func Language(baseData []byte, lang, version, source string, rows int, choices []pool.Choice) ([]byte, error) {
	var b strings.Builder
	b.WriteString(overrideHeader(lang, version, source, rows, choices))
	b.WriteString("\nlanguages:\n  " + lang + ":\n    thresholds:\n")
	for i := range choices {
		c := &choices[i]
		if !report.Overridden(c) {
			continue
		}
		at, name, unit := c.Stats.P90, "p90", "packages"
		if c.Pool == pool.PoolFunctions {
			at, name, unit = c.Stats.P99, "p99", "functions"
		}
		fmt.Fprintf(&b, "      # %s %s over %d %s; the top level has max %s, max_delta %s.\n",
			name, report.Num(at), c.Stats.N, unit, report.Opt(c.Rule.Max), report.Opt(c.Rule.MaxDelta))
		fmt.Fprintf(&b, "      - metric: %s\n        kind: %s\n", c.Rule.Metric, c.Rule.Kind)
		if c.MaxDelta != nil {
			fmt.Fprintf(&b, "        max_delta: %s\n", report.Num(*c.MaxDelta))
		}
		if c.Rule.RatchetFromZero {
			b.WriteString("        ratchet_from_zero: true\n")
		}
		if c.Max != nil {
			fmt.Fprintf(&b, "        max: %s\n", report.Num(*c.Max))
		}
		if c.Rule.Kind == gate.Capacity {
			fmt.Fprintf(&b, "        warn_at: %s\n", report.Num(c.Rule.WarnAt))
		}
	}
	out := []byte(b.String())
	merged, err := MergeLanguage(baseData, lang, out)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Parse(merged)
	if err != nil {
		return nil, fmt.Errorf("override does not validate: %w", err)
	}
	if got := cfg.ForLanguage(lang).Version; got != version {
		return nil, fmt.Errorf("override resolves to config_version %q, want %q", got, version)
	}
	return out, nil
}

// overrideHeader is the override block's leading comment.
func overrideHeader(lang, version, source string, rows int, choices []pool.Choice) string {
	var inherited []string
	for i := range choices {
		if !report.Overridden(&choices[i]) {
			inherited = append(inherited, choices[i].Rule.Metric)
		}
	}
	lines := []string{"Astimate per-language threshold override, to paste into the configuration.", ""}
	lines = append(lines, report.Wrap(fmt.Sprintf("Override for %s (reports show %s), fitted by calibration/fit (SPEC.md 11.1 and 13) from %d packages in %s.",
		lang, version, rows, filepath.ToSlash(source)), 74)...)
	for _, m := range report.MethodText() {
		lines = append(lines, "")
		lines = append(lines, report.Wrap(m, 74)...)
	}
	lines = append(lines, "")
	lines = append(lines, report.Wrap("Each rule below replaces the top-level rule on its metric for "+lang+
		". A rule the data fitted no statistic for keeps the top-level rule: "+strings.Join(inherited, ", ")+
		". The rebuild parameters are not overridden: they are calibrated by rebuild experiments (SPEC.md 11.2), not by a corpus.", 74)...)
	for i, l := range lines {
		lines[i] = strings.TrimRight("# "+l, " ")
	}
	return strings.Join(lines, "\n") + "\n"
}

// decodeMapping unmarshals data as a YAML document holding a single
// mapping node, the shape every base configuration must have.
func decodeMapping(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decoding base config: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("base config is not a mapping")
	}
	return &doc, nil
}

// MergeLanguage returns the base configuration with its languages.<lang>
// entry replaced by the one in block, or added, for validation.
func MergeLanguage(baseData []byte, lang string, block []byte) ([]byte, error) {
	doc, err := decodeMapping(baseData)
	if err != nil {
		return nil, err
	}
	var over yaml.Node
	if err := yaml.Unmarshal(block, &over); err != nil {
		return nil, fmt.Errorf("decoding override: %w", err)
	}
	var entry *yaml.Node
	if len(over.Content) == 1 {
		if langs := value(over.Content[0], "languages"); langs != nil {
			entry = value(langs, lang)
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("override has no languages.%s", lang)
	}
	root := doc.Content[0]
	langs := value(root, "languages")
	if langs == nil || langs.Kind != yaml.MappingNode {
		if langs == nil {
			langs = &yaml.Node{}
			root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "languages"}, langs)
		}
		*langs = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	if v := value(langs, lang); v != nil {
		*v = *entry
	} else {
		langs.Content = append(langs.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: lang}, entry)
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encoding merged config: %w", err)
	}
	return out, nil
}

// Candidate rewrites the base configuration file with the fitted
// values: config_version becomes version, and each threshold's existing max
// and max_delta take the matching choice's values. Every other key, and
// the comments on them, are copied unchanged; the file's leading comment
// is replaced with header.
func Candidate(base []byte, version, header string, choices []pool.Choice) ([]byte, error) {
	doc, err := decodeMapping(base)
	if err != nil {
		return nil, err
	}
	root := doc.Content[0]
	// The file's leading comment sits on the document when a blank line
	// separates it from the first key's own comment, else on that key.
	if doc.HeadComment == "" {
		root.Content[0].HeadComment = ""
	}
	doc.HeadComment = header

	v := value(root, "config_version")
	if v == nil {
		return nil, errors.New("base config has no config_version")
	}
	setScalar(v, version, "!!str")

	rules := value(root, "thresholds")
	if rules == nil || rules.Kind != yaml.SequenceNode || len(rules.Content) != len(choices) {
		return nil, errors.New("base thresholds do not match the fitted rules")
	}
	for i, item := range rules.Content {
		c := choices[i]
		if m := value(item, "metric"); m == nil || m.Value != c.Rule.Metric {
			return nil, fmt.Errorf("thresholds[%d]: base rule does not match %q", i, c.Rule.Metric)
		}
		keys := [2]string{"max", "max_delta"}
		vals := [2]*float64{c.Max, c.MaxDelta}
		for j, key := range keys {
			if err := setNumber(item, key, vals[j]); err != nil {
				return nil, fmt.Errorf("thresholds[%d] %q: %w", i, c.Rule.Metric, err)
			}
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encoding candidate: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encoding candidate: %w", err)
	}
	return spaceSections(buf.Bytes()), nil
}

// spaceSections restores the blank line the encoder drops before each
// top-level comment block that follows a key.
func spaceSections(data []byte) []byte {
	lines := strings.SplitAfter(string(data), "\n")
	var b strings.Builder
	b.Grow(len(data) + 64)
	prev := ""
	for _, l := range lines {
		if strings.HasPrefix(l, "#") && prev != "" && prev != "\n" && !strings.HasPrefix(prev, "#") {
			b.WriteByte('\n')
		}
		b.WriteString(l)
		prev = l
	}
	return []byte(b.String())
}

// value returns the value node of key in mapping m, or nil.
func value(m *yaml.Node, key string) *yaml.Node {
	if m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setNumber sets key's value in mapping m to v. A key the base leaves out
// stays out, and so does a nil v; a base key with no fitted value is an
// error, since the fit would silently keep an uncalibrated number.
func setNumber(m *yaml.Node, key string, v *float64) error {
	n := value(m, key)
	switch {
	case n == nil && v == nil:
		return nil
	case n == nil:
		return fmt.Errorf("fitted %s the base rule does not have", key)
	case v == nil:
		return fmt.Errorf("base %s has no fitted value", key)
	}
	s := strconv.FormatFloat(*v, 'f', -1, 64)
	tag := "!!int"
	if strings.Contains(s, ".") {
		tag = "!!float"
	}
	setScalar(n, s, tag)
	return nil
}

// setScalar makes n a plain scalar with value s and tag, keeping its
// comments.
func setScalar(n *yaml.Node, s, tag string) {
	n.Kind = yaml.ScalarNode
	n.Style = 0
	n.Tag = tag
	n.Value = s
}
