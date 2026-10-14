package config

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Churn is the optional churn section (SPEC.md 6.6, section 9). A key
// absent from the file takes the value of the embedded default.
type Churn struct {
	// Commits is the window: how many first-parent commits from HEAD
	// churn_commits and churn_authors count. 0 turns churn off, leaving
	// both metrics null.
	Commits int
}

// fileChurn mirrors the churn section.
type fileChurn struct {
	Commits *int `yaml:"commits"`
}

// buildChurn fills cfg.Churn from the churn section, taking an absent
// section or key from the embedded default.
func (fc *fileConfig) buildChurn(cfg *Config) error {
	if fc.Churn != nil && fc.Churn.Commits != nil {
		cfg.Churn.Commits = *fc.Churn.Commits
		return nil
	}
	var def fileConfig
	if err := yaml.Unmarshal([]byte(defaultYAML), &def); err != nil {
		return fmt.Errorf("embedded default: %w", err)
	}
	if def.Churn == nil || def.Churn.Commits == nil {
		return errors.New("embedded default: churn section is incomplete")
	}
	cfg.Churn.Commits = *def.Churn.Commits
	return nil
}

// validateChurn reports a negative window.
func (c *Config) validateChurn() error {
	if c.Churn.Commits < 0 {
		return fmt.Errorf("churn.commits must be >= 0, got %d", c.Churn.Commits)
	}
	return nil
}
