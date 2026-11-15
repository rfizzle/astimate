package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/rfizzle/astimate/internal/gate/exempt"
)

// ExemptionsFileName is the project-owned exemptions file (SPEC.md 8.6):
// beside the configuration file in use, or in the module root under the
// embedded default. It holds only an exemptions list, so a project whose
// configuration comes from a shared kit records its own exemptions without
// editing that configuration. The name is fixed: a key naming it inside
// the configuration would need the protected file edited.
const ExemptionsFileName = "astimate.exemptions.yaml"

// exemptionsKey is the one top-level key the exemptions file may set.
const exemptionsKey = "exemptions"

// fileExemptions mirrors the exemptions file.
type fileExemptions struct {
	Exemptions []fileExemption `yaml:"exemptions"`
}

// ForModule returns the configuration the module at root is checked with.
// Resolve reads the exemptions file beside a configuration file at once, and
// Parse and Load never read one, so such a configuration is returned
// unchanged. The embedded default Resolve fell back to has no file to sit
// beside, so for it ForModule reads the exemptions file in root and
// returns a copy with its entries appended after the configuration's own;
// c itself is never modified, so a configuration shared between modules
// (one MCP server checking several) stays as resolved.
func (c *Config) ForModule(root string) (*Config, error) {
	if !c.exemptionsAtRoot {
		return c, nil
	}
	extra, err := c.readExemptions(root)
	if err != nil {
		return nil, err
	}
	cp := *c
	cp.exemptionsAtRoot = false
	cp.Exemptions = slices.Concat(c.Exemptions, extra)
	return &cp, nil
}

// loadWithExemptions loads the configuration file at path and appends the
// entries of the exemptions file beside it.
func loadWithExemptions(path string) (*Config, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	extra, err := cfg.readExemptions(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	cfg.Exemptions = append(cfg.Exemptions, extra...)
	return cfg, nil
}

// readExemptions reads the exemptions file in dir and returns its entries,
// each validated against c as the configuration's own are and carrying the
// file's name as its Source. A missing or empty file, or an empty list,
// yields none. Any top-level key but exemptions is an error naming the
// file and the key, and an invalid entry's errors name the file and the
// entry's index within it.
func (c *Config) readExemptions(dir string) ([]exempt.Entry, error) {
	path := filepath.Join(dir, ExemptionsFileName)
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("reading exemptions: %w", err)
	}
	var keys map[string]any
	if err := yaml.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("%s: want a mapping holding only %s: %w", path, exemptionsKey, err)
	}
	others := make([]string, 0, len(keys))
	for k := range keys {
		if k != exemptionsKey {
			others = append(others, k)
		}
	}
	slices.Sort(others)
	errs := make([]error, 0, len(others))
	for _, k := range others {
		errs = append(errs, fmt.Errorf("%s: %s: the file holds only %s; set %s in %s", path, k, exemptionsKey, k, FileName))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var fe fileExemptions
	if err := dec.Decode(&fe); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decoding %s: %w", path, err)
	}
	gated := c.gatedMetric()
	out := make([]exempt.Entry, 0, len(fe.Exemptions))
	for i, e := range fe.Exemptions {
		entry := exempt.Entry{Package: e.Package, Metric: e.Metric, Reason: e.Reason, Expires: e.Expires, Source: ExemptionsFileName}
		errs = append(errs, prefixEach(fmt.Sprintf("%s: exemptions[%d]", path, i), entry.Validate(gated))...)
		out = append(out, entry)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return out, nil
}
