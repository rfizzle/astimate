package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Hook is the optional hook section (SPEC.md 8.5 and section 9): settings
// of `check --format hook` that do not change its decision.
type Hook struct {
	// BlockLog is the block log's path, relative to the module root unless
	// absolute: the file each hook-format check appends its block and pass
	// lines to. Empty, the default, leaves the log off.
	BlockLog string
}

// fileHook mirrors the hook section.
type fileHook struct {
	BlockLog *string `yaml:"block_log"`
}

// validateHook reports a block log path that names no file: one with
// surrounding blanks, or one that ends in a separator, ".", or "..".
func (c *Config) validateHook() error {
	p := c.Hook.BlockLog
	if p == "" {
		return nil
	}
	base := filepath.Base(p)
	if strings.TrimSpace(p) != p || strings.HasSuffix(p, "/") || strings.HasSuffix(p, string(filepath.Separator)) ||
		base == "." || base == ".." {
		return fmt.Errorf("hook.block_log %q: want a file path such as .astimate/blocks.jsonl", p)
	}
	return nil
}
