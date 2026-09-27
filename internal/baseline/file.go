package baseline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rfizzle/astimate/internal/metrics"
)

// DefaultPath is where `astimate baseline write` puts the baseline file,
// relative to the module root.
const DefaultPath = ".astimate/baseline.json"

// DefaultTokenizer is the tokenizer FromFile reports for a file that
// records none, as files written before the field existed do.
const DefaultTokenizer = "est"

// fileFormat is the JSON layout of a baseline file (SPEC.md 8.3).
type fileFormat struct {
	Ref         string                        `json:"ref"`
	GeneratedAt time.Time                     `json:"generated_at"`
	ModulePath  string                        `json:"module_path"`
	Tokenizer   string                        `json:"tokenizer,omitempty"`
	Packages    map[string]metrics.RawMetrics `json:"packages"`
}

// FromFile reads a baseline file written by Write. Its Ref and Tokenizer
// are the ones recorded in the file; a file that records no tokenizer
// reports DefaultTokenizer.
func FromFile(path string) (Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading baseline: %w", err)
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("decoding baseline %s: %w", path, err)
	}
	if f.Packages == nil {
		f.Packages = map[string]metrics.RawMetrics{}
	}
	if f.Tokenizer == "" {
		f.Tokenizer = DefaultTokenizer
	}
	return &snapshot{ref: f.Ref, tokenizer: f.Tokenizer, pkgs: f.Packages}, nil
}

// Write stores pkgs, keyed by import path, as a baseline file at path with
// mode 0644, recording ref, modulePath, tokenizer (the method that counted
// the packages' tokens_est; empty records DefaultTokenizer) and the current
// time. It writes a temporary file in the same directory and renames it into
// place, so a reader never sees a partial file. The directory must exist.
func Write(path, ref, modulePath, tokenizer string, pkgs map[string]metrics.RawMetrics) (err error) {
	if pkgs == nil {
		pkgs = map[string]metrics.RawMetrics{}
	}
	if tokenizer == "" {
		tokenizer = DefaultTokenizer
	}
	data, err := json.MarshalIndent(fileFormat{
		Ref:         ref,
		GeneratedAt: time.Now().UTC().Truncate(time.Second),
		ModulePath:  modulePath,
		Tokenizer:   tokenizer,
		Packages:    pkgs,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding baseline: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("writing baseline %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("writing baseline %s: %w", path, err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		return fmt.Errorf("writing baseline %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing baseline %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("writing baseline %s: %w", path, err)
	}
	return nil
}
