// Package config holds the embedded default configuration: composite weights
// and gate thresholds in one commented YAML file.
package config

import _ "embed"

//go:embed default.yaml
var defaultYAML string

// Default returns the embedded default configuration file. Each call returns a
// fresh copy, so callers may modify the result.
func Default() []byte {
	return []byte(defaultYAML)
}
