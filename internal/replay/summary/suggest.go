package summary

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/rfizzle/astimate/internal/config"
)

// suggestComment is the line comment a suggestion puts on each severity it
// sets.
const suggestComment = "set by astimate dryrun: over its failure budget"

// Suggest returns the configuration file src with `severity: warn` set on
// every rule on a metric of warn, at the top level and in every languages
// override, its comments kept; a rule that disables its metric for a
// language is left alone. Every other rule keeps its severity. With no
// metric to warn on, src is returned unchanged. The result is parsed with
// config.Parse before it is returned, so it is a valid configuration.
func Suggest(src []byte, warn []string) ([]byte, error) {
	if len(warn) == 0 {
		return src, nil
	}
	doc := new(yaml.Node)
	err := yaml.NewDecoder(bytes.NewReader(src)).Decode(doc)
	root := topMapping(doc)
	if err != nil || root == nil {
		return nil, errors.Join(errors.New("decoding config: not a YAML mapping"), err)
	}
	setWarn(mapValue(root, "thresholds"), warn)
	if langs := mapValue(root, "languages"); langs != nil && langs.Kind == yaml.MappingNode {
		for i := 1; i < len(langs.Content); i += 2 {
			setWarn(mapValue(langs.Content[i], "thresholds"), warn)
		}
	}
	// Two-space indentation, as config init writes the file.
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	err = errors.Join(enc.Encode(doc), enc.Close())
	if err == nil {
		_, err = config.Parse(out.Bytes())
	}
	if err != nil {
		return nil, fmt.Errorf("suggested configuration: %w", err)
	}
	return out.Bytes(), nil
}

// topMapping returns the mapping a YAML document node holds, nil when it
// holds anything else.
func topMapping(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 && doc.Content[0].Kind == yaml.MappingNode {
		return doc.Content[0]
	}
	return nil
}

// setWarn sets severity warn on each rule of the sequence rules whose
// metric is in warn and that does not disable it.
func setWarn(rules *yaml.Node, warn []string) {
	if rules == nil || rules.Kind != yaml.SequenceNode {
		return
	}
	for _, rule := range rules.Content {
		metric := mapValue(rule, "metric")
		if metric == nil || !slices.Contains(warn, metric.Value) {
			continue
		}
		if d := mapValue(rule, "disabled"); d != nil && d.Value == "true" {
			continue
		}
		if sev := mapValue(rule, "severity"); sev != nil {
			sev.Value, sev.Tag, sev.Style, sev.LineComment = "warn", "!!str", 0, suggestComment
			continue
		}
		rule.Content = append(rule.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "severity"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "warn", LineComment: suggestComment})
	}
}

// mapValue returns the value of key in the mapping node m, nil when m is
// not a mapping or has no such key.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	// Content alternates keys and values.
	for pair := range slices.Chunk(m.Content, 2) {
		if len(pair) == 2 && pair[0].Value == key {
			return pair[1]
		}
	}
	return nil
}
