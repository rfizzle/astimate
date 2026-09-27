// Package config loads the unified configuration: rebuild parameters and gate
// thresholds in one commented YAML file, with the defaults embedded.
package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

//go:embed default.yaml
var defaultYAML string

// FileName is the config file Resolve looks for in the working directory.
const FileName = "astimate.yaml"

// Sources reported by Resolve.
const (
	// SourceFlag means the config came from the --config flag.
	SourceFlag = "flag"
	// SourceWorkDir means the config came from ./astimate.yaml.
	SourceWorkDir = "./" + FileName
	// SourceEmbedded means the embedded default was used.
	SourceEmbedded = "embedded"
)

// defaultWarnAt is the capacity warn_at applied when a rule omits it
// (SPEC.md section 8.1).
const defaultWarnAt = 0.75

// Config is a parsed and validated configuration.
type Config struct {
	// Version identifies the calibration the values came from.
	Version string
	// CharsPerToken is bytes of source per estimated token.
	CharsPerToken float64
	// DupMinTokens is the minimum normalized token run counted as a
	// duplicate block.
	DupMinTokens int
	// DupIgnoreLiteralOnly drops a duplicate block made only of literals
	// and literal-table punctuation.
	DupIgnoreLiteralOnly bool
	// DupFoldSigns counts a unary + or - directly before a numeric literal
	// as part of the literal when dropping literal-only blocks.
	DupFoldSigns bool
	// Rebuild holds the rebuild-estimate parameters.
	Rebuild score.RebuildParams
	// Thresholds holds the gate rules in file order.
	Thresholds []gate.Threshold
}

// fileConfig mirrors the YAML schema. Pointers distinguish a missing scalar
// from an explicit zero so missing fields fail instead of defaulting to 0.
type fileConfig struct {
	ConfigVersion        string          `yaml:"config_version"`
	CharsPerToken        *float64        `yaml:"chars_per_token"`
	DupMinTokens         *int            `yaml:"dup_min_tokens"`
	DupIgnoreLiteralOnly *bool           `yaml:"dup_ignore_literal_only"`
	DupFoldSigns         *bool           `yaml:"dup_fold_signs"`
	Rebuild              *fileRebuild    `yaml:"rebuild"`
	Thresholds           []fileThreshold `yaml:"thresholds"`
}

type fileRebuild struct {
	ContextBudget           *float64   `yaml:"context_budget"`
	TokensPerExport         *float64   `yaml:"tokens_per_export"`
	TokensPerUntestedExport *float64   `yaml:"tokens_per_untested_export"`
	TokensPerHiddenState    *float64   `yaml:"tokens_per_hidden_state"`
	SuperlinearExponent     *float64   `yaml:"superlinear_exponent"`
	CocomoA                 *float64   `yaml:"cocomo_a"`
	CocomoB                 *float64   `yaml:"cocomo_b"`
	DaysPerMonth            *float64   `yaml:"days_per_month"`
	Tiers                   *fileTiers `yaml:"tiers"`
}

type fileTiers struct {
	OnePassMax   *float64 `yaml:"one_pass_max"`
	FewPassesMax *float64 `yaml:"few_passes_max"`
}

type fileThreshold struct {
	Metric          string   `yaml:"metric"`
	Kind            string   `yaml:"kind"`
	Max             *float64 `yaml:"max"`
	MaxDelta        *float64 `yaml:"max_delta"`
	RatchetFromZero bool     `yaml:"ratchet_from_zero"`
	WarnAt          *float64 `yaml:"warn_at"`
	Require         *bool    `yaml:"require"`
	When            string   `yaml:"when"`
}

// Default returns the embedded default configuration file. Each call returns a
// fresh copy, so callers may modify the result.
func Default() []byte {
	return []byte(defaultYAML)
}

// Parse decodes and validates a configuration. Unknown keys, missing fields
// and out-of-range values are errors naming the offending field or metric.
func Parse(data []byte) (*Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var fc fileConfig
	if err := dec.Decode(&fc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("decoding config: empty document")
		}
		return nil, fmt.Errorf("decoding config: %w", err)
	}
	cfg, err := fc.build()
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Load reads and parses the configuration file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	return cfg, nil
}

// Resolve picks the configuration by precedence: flagPath when non-empty,
// then ./astimate.yaml, then the embedded default. It returns the config and
// the source used: SourceFlag, SourceWorkDir or SourceEmbedded.
func Resolve(flagPath string) (*Config, string, error) {
	return resolveIn(flagPath, ".")
}

// resolveIn is Resolve with the working directory made explicit for tests.
func resolveIn(flagPath, dir string) (*Config, string, error) {
	if flagPath != "" {
		cfg, err := Load(flagPath)
		return cfg, SourceFlag, err
	}
	local := filepath.Join(dir, FileName)
	switch _, err := os.Stat(local); {
	case err == nil:
		cfg, err := Load(local)
		return cfg, SourceWorkDir, err
	case !errors.Is(err, fs.ErrNotExist):
		return nil, "", fmt.Errorf("checking %s: %w", local, err)
	}
	cfg, err := Parse(Default())
	if err != nil {
		return nil, SourceEmbedded, fmt.Errorf("embedded default: %w", err)
	}
	return cfg, SourceEmbedded, nil
}

// isRebuildOutput reports names that are estimate results, not raw metrics;
// SPEC.md section 8.1 rules out gating them directly.
func isRebuildOutput(name string) bool {
	switch name {
	case "agent_passes", "human_days", "rebuild_tokens":
		return true
	}
	return false
}

// Validate checks the top-level scalars, the rebuild parameters and every
// threshold, joining all errors.
func (c *Config) Validate() error {
	var errs []error
	if c.Version == "" {
		errs = append(errs, errors.New("config_version is required"))
	}
	if c.CharsPerToken <= 0 {
		errs = append(errs, fmt.Errorf("chars_per_token must be > 0, got %v", c.CharsPerToken))
	}
	if c.DupMinTokens <= 0 {
		errs = append(errs, fmt.Errorf("dup_min_tokens must be > 0, got %d", c.DupMinTokens))
	}
	if err := c.Rebuild.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("rebuild: %w", err))
	}
	names := metrics.MetricNames()
	known := make(map[string]bool, len(names))
	for _, n := range names {
		known[n] = true
	}
	isKnown := func(n string) bool { return known[n] }
	for i, t := range c.Thresholds {
		if isRebuildOutput(t.Metric) {
			errs = append(errs, fmt.Errorf("thresholds[%d]: %q is a rebuild output and cannot be gated", i, t.Metric))
			continue
		}
		if err := t.Validate(isKnown); err != nil {
			errs = append(errs, fmt.Errorf("thresholds[%d]: %w", i, err))
		}
	}
	return errors.Join(errs...)
}

// build converts the decoded file into a Config, reporting missing fields and
// malformed when guards. Range checks are left to Validate.
func (fc *fileConfig) build() (*Config, error) {
	var errs []error
	missing := func(name string) { errs = append(errs, fmt.Errorf("%s is required", name)) }
	cfg := &Config{Version: fc.ConfigVersion}
	if fc.CharsPerToken == nil {
		missing("chars_per_token")
	} else {
		cfg.CharsPerToken = *fc.CharsPerToken
	}
	if fc.DupMinTokens == nil {
		missing("dup_min_tokens")
	} else {
		cfg.DupMinTokens = *fc.DupMinTokens
	}
	if fc.DupIgnoreLiteralOnly == nil {
		missing("dup_ignore_literal_only")
	} else {
		cfg.DupIgnoreLiteralOnly = *fc.DupIgnoreLiteralOnly
	}
	if fc.DupFoldSigns == nil {
		missing("dup_fold_signs")
	} else {
		cfg.DupFoldSigns = *fc.DupFoldSigns
	}
	if fc.Rebuild == nil {
		missing("rebuild")
	} else {
		r := fc.Rebuild
		for _, f := range []struct {
			name string
			src  *float64
			dst  *float64
		}{
			{"rebuild.context_budget", r.ContextBudget, &cfg.Rebuild.ContextBudget},
			{"rebuild.tokens_per_export", r.TokensPerExport, &cfg.Rebuild.TokensPerExport},
			{"rebuild.tokens_per_untested_export", r.TokensPerUntestedExport, &cfg.Rebuild.TokensPerUntestedExport},
			{"rebuild.tokens_per_hidden_state", r.TokensPerHiddenState, &cfg.Rebuild.TokensPerHiddenState},
			{"rebuild.superlinear_exponent", r.SuperlinearExponent, &cfg.Rebuild.SuperlinearExponent},
			{"rebuild.cocomo_a", r.CocomoA, &cfg.Rebuild.CocomoA},
			{"rebuild.cocomo_b", r.CocomoB, &cfg.Rebuild.CocomoB},
			{"rebuild.days_per_month", r.DaysPerMonth, &cfg.Rebuild.DaysPerMonth},
		} {
			if f.src == nil {
				missing(f.name)
				continue
			}
			*f.dst = *f.src
		}
		if r.Tiers == nil {
			missing("rebuild.tiers")
		} else {
			if r.Tiers.OnePassMax == nil {
				missing("rebuild.tiers.one_pass_max")
			} else {
				cfg.Rebuild.Tiers.OnePassMax = *r.Tiers.OnePassMax
			}
			if r.Tiers.FewPassesMax == nil {
				missing("rebuild.tiers.few_passes_max")
			} else {
				cfg.Rebuild.Tiers.FewPassesMax = *r.Tiers.FewPassesMax
			}
		}
	}
	cfg.Thresholds = make([]gate.Threshold, 0, len(fc.Thresholds))
	for i, ft := range fc.Thresholds {
		t := gate.Threshold{
			Metric:          ft.Metric,
			Kind:            gate.Kind(ft.Kind),
			Max:             ft.Max,
			MaxDelta:        ft.MaxDelta,
			Require:         ft.Require,
			RatchetFromZero: ft.RatchetFromZero,
		}
		switch {
		case ft.WarnAt != nil:
			t.WarnAt = *ft.WarnAt
		case t.Kind == gate.Capacity:
			t.WarnAt = defaultWarnAt
		}
		if ft.When != "" {
			cond, err := gate.ParseCondition(ft.When)
			if err != nil {
				errs = append(errs, fmt.Errorf("thresholds[%d] %q: when: %w", i, ft.Metric, err))
			} else {
				t.When = &cond
			}
		}
		cfg.Thresholds = append(cfg.Thresholds, t)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return cfg, nil
}
