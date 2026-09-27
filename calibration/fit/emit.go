package main

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// emitCandidate rewrites the base configuration file with the fitted
// values: config_version becomes version, and each threshold's existing max
// and max_delta take the matching choice's values. Every other key, and
// the comments on them, are copied unchanged; the file's leading comment
// is replaced with header.
func emitCandidate(base []byte, version, header string, choices []Choice) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(base, &doc); err != nil {
		return nil, fmt.Errorf("decoding base config: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("base config is not a mapping")
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
		if err := setNumber(item, "max", c.Max); err != nil {
			return nil, fmt.Errorf("thresholds[%d] %q: %w", i, c.Rule.Metric, err)
		}
		if err := setNumber(item, "max_delta", c.MaxDelta); err != nil {
			return nil, fmt.Errorf("thresholds[%d] %q: %w", i, c.Rule.Metric, err)
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
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
