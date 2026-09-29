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
	"strings"

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
	// Duplication holds the duplicate-block detection settings.
	Duplication Duplication
	// Rebuild holds the rebuild-estimate parameters.
	Rebuild score.RebuildParams
	// Thresholds holds the gate rules in file order.
	Thresholds []gate.Threshold
	// Exemptions holds the exemptions in file order (SPEC.md 8.6). They
	// apply to every language: a languages override carries none.
	Exemptions []gate.Exemption
	// Warnings are non-fatal findings from parsing, such as deprecated keys.
	// Callers log each one once.
	Warnings []string

	// languages holds the per-language overrides by language id; ForLanguage
	// applies them.
	languages map[string]languageOverride
}

// Duplication is the optional duplication section. Keys absent from the file
// take the values of the embedded default.
type Duplication struct {
	// MinTokens is the minimum normalized token run counted as a duplicate
	// block.
	MinTokens int
	// IgnoreLiteralOnly drops a duplicate block made only of literals and
	// literal-table punctuation.
	IgnoreLiteralOnly bool
	// FoldSigns counts a unary + or - directly before a numeric literal as
	// part of the literal when dropping literal-only blocks.
	FoldSigns bool
	// SplitLiteralRuns cuts each duplicate block, under IgnoreLiteralOnly,
	// at every run of at least MinTokens literal-only tokens and keeps the
	// parts of at least MinTokens tokens.
	SplitLiteralRuns bool
}

// fileConfig mirrors the YAML schema. Pointers distinguish a missing scalar
// from an explicit zero so missing required fields fail instead of
// defaulting to 0, and missing optional ones take the embedded default.
type fileConfig struct {
	ConfigVersion string           `yaml:"config_version"`
	CharsPerToken *float64         `yaml:"chars_per_token"`
	Duplication   *fileDuplication `yaml:"duplication"`
	Rebuild       *fileRebuild     `yaml:"rebuild"`
	Thresholds    []fileThreshold  `yaml:"thresholds"`
	Exemptions    []fileExemption  `yaml:"exemptions"`
	// Languages holds the optional per-language overrides by language id.
	Languages map[string]*fileLanguage `yaml:"languages"`

	// Deprecated top-level spellings of the duplication section, accepted
	// with a warning for one release.
	DupMinTokens         *int  `yaml:"dup_min_tokens"`
	DupIgnoreLiteralOnly *bool `yaml:"dup_ignore_literal_only"`
	DupFoldSigns         *bool `yaml:"dup_fold_signs"`
}

type fileDuplication struct {
	MinTokens         *int  `yaml:"min_tokens"`
	IgnoreLiteralOnly *bool `yaml:"ignore_literal_only"`
	FoldSigns         *bool `yaml:"fold_signs"`
	SplitLiteralRuns  *bool `yaml:"split_literal_runs"`
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
	OverMaxDelta    *float64 `yaml:"over_max_delta"`
	Require         *bool    `yaml:"require"`
	When            string   `yaml:"when"`
	// Disabled drops the top-level rule on Metric; only under languages.
	Disabled bool `yaml:"disabled"`
}

// fileExemption mirrors one entry of the exemptions section.
type fileExemption struct {
	Package string `yaml:"package"`
	Metric  string `yaml:"metric"`
	Reason  string `yaml:"reason"`
	// Expires is a YYYY-MM-DD date; YAML resolves an unquoted date as a
	// timestamp, which decodes into a string as written.
	Expires string `yaml:"expires"`
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

// Validate checks the top-level scalars, the rebuild parameters, every
// threshold and every exemption, joining all errors. An exemption's errors
// name its index, as in "exemptions[2]: reason is required", and it may
// name only a metric some rule gates, at the top level or in a languages
// override.
func (c *Config) Validate() error {
	var errs []error
	if c.Version == "" {
		errs = append(errs, errors.New("config_version is required"))
	}
	if c.CharsPerToken <= 0 {
		errs = append(errs, fmt.Errorf("chars_per_token must be > 0, got %v", c.CharsPerToken))
	}
	if c.Duplication.MinTokens <= 0 {
		errs = append(errs, fmt.Errorf("duplication.min_tokens must be > 0, got %d", c.Duplication.MinTokens))
	}
	if err := c.Rebuild.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("rebuild: %w", err))
	}
	isKnown := knownMetric()
	for i, t := range c.Thresholds {
		if err := validateRule(t, isKnown); err != nil {
			errs = append(errs, fmt.Errorf("thresholds[%d]: %w", i, err))
		}
	}
	errs = append(errs, c.validateLanguages(isKnown)...)
	gated := c.gatedMetric()
	for i, e := range c.Exemptions {
		errs = append(errs, prefixEach(fmt.Sprintf("exemptions[%d]", i), e.Validate(gated))...)
	}
	return errors.Join(errs...)
}

// prefixEach returns each error err joins (errors.Join), or err alone,
// prefixed with prefix, so every line of a multi-error names what it is
// about; nil for a nil err.
func prefixEach(prefix string, err error) []error {
	if err == nil {
		return nil
	}
	errs := []error{err}
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		errs = j.Unwrap()
	}
	out := make([]error, 0, len(errs))
	for _, e := range errs {
		out = append(out, fmt.Errorf("%s: %w", prefix, e))
	}
	return out
}

// gatedMetric returns a lookup of the metrics some rule gates, at the top
// level or in any language override, which are the metrics an exemption may
// name.
func (c *Config) gatedMetric() func(string) bool {
	gated := make(map[string]bool, len(c.Thresholds))
	for _, t := range c.Thresholds {
		gated[t.Metric] = true
	}
	for _, o := range c.languages {
		for _, t := range o.rules {
			gated[t.Metric] = true
		}
	}
	return func(m string) bool { return gated[m] }
}

// knownMetric returns a lookup of the RawMetrics field names.
func knownMetric() func(string) bool {
	names := metrics.MetricNames()
	known := make(map[string]bool, len(names))
	for _, n := range names {
		known[n] = true
	}
	return func(n string) bool { return known[n] }
}

// validateRule checks one rule, rejecting rebuild outputs as its metric.
func validateRule(t gate.Threshold, isKnown func(string) bool) error {
	if isRebuildOutput(t.Metric) {
		return fmt.Errorf("%q is a rebuild output and cannot be gated", t.Metric)
	}
	return t.Validate(isKnown)
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
	if err := fc.buildDuplication(cfg); err != nil {
		errs = append(errs, err)
	}
	if fc.Rebuild == nil {
		missing("rebuild")
	} else {
		if fc.Rebuild.Tiers == nil {
			missing("rebuild.tiers")
		}
		for _, f := range fc.Rebuild.fields(&cfg.Rebuild) {
			switch {
			case f.src != nil:
				*f.dst = *f.src
			case fc.Rebuild.Tiers != nil || !strings.HasPrefix(f.name, "tiers."):
				missing("rebuild." + f.name)
			}
		}
	}
	cfg.Thresholds = make([]gate.Threshold, 0, len(fc.Thresholds))
	for i, ft := range fc.Thresholds {
		prefix := fmt.Sprintf("thresholds[%d] %q", i, ft.Metric)
		if ft.Disabled {
			errs = append(errs, fmt.Errorf("%s: disabled applies only under languages.<language>.thresholds", prefix))
			continue
		}
		t, err := ft.build(prefix)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		cfg.Thresholds = append(cfg.Thresholds, t)
	}
	if len(fc.Exemptions) > 0 {
		cfg.Exemptions = make([]gate.Exemption, 0, len(fc.Exemptions))
		for _, fe := range fc.Exemptions {
			cfg.Exemptions = append(cfg.Exemptions, gate.Exemption{
				Package: fe.Package, Metric: fe.Metric, Reason: fe.Reason, Expires: fe.Expires,
			})
		}
	}
	if err := fc.buildLanguages(cfg); err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return cfg, nil
}

// rebuildField pairs a rebuild parameter's key under the rebuild section
// with its value in the file, nil when absent, and its field in the params.
type rebuildField struct {
	name string
	src  *float64
	dst  *float64
}

// fields lists every rebuild parameter of r against its field in p, tiers
// included; a missing tiers section lists its keys as absent.
func (r *fileRebuild) fields(p *score.RebuildParams) []rebuildField {
	tiers := r.Tiers
	if tiers == nil {
		tiers = &fileTiers{}
	}
	return []rebuildField{
		{"context_budget", r.ContextBudget, &p.ContextBudget},
		{"tokens_per_export", r.TokensPerExport, &p.TokensPerExport},
		{"tokens_per_untested_export", r.TokensPerUntestedExport, &p.TokensPerUntestedExport},
		{"tokens_per_hidden_state", r.TokensPerHiddenState, &p.TokensPerHiddenState},
		{"superlinear_exponent", r.SuperlinearExponent, &p.SuperlinearExponent},
		{"cocomo_a", r.CocomoA, &p.CocomoA},
		{"cocomo_b", r.CocomoB, &p.CocomoB},
		{"days_per_month", r.DaysPerMonth, &p.DaysPerMonth},
		{"tiers.one_pass_max", tiers.OnePassMax, &p.Tiers.OnePassMax},
		{"tiers.few_passes_max", tiers.FewPassesMax, &p.Tiers.FewPassesMax},
	}
}

// build converts one decoded rule into a gate.Threshold, applying the
// capacity warn_at default. A malformed when guard is an error prefixed
// with prefix. Range checks are left to Validate.
func (ft fileThreshold) build(prefix string) (gate.Threshold, error) {
	t := gate.Threshold{
		Metric:          ft.Metric,
		Kind:            gate.Kind(ft.Kind),
		Max:             ft.Max,
		MaxDelta:        ft.MaxDelta,
		OverMaxDelta:    ft.OverMaxDelta,
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
			return t, fmt.Errorf("%s: when: %w", prefix, err)
		}
		t.When = &cond
	}
	return t, nil
}

// legacyDupKey pairs a deprecated top-level key with its section key.
type legacyDupKey struct {
	old, key string
	set      bool
}

// buildDuplication fills cfg.Duplication from the duplication section, or
// from the deprecated top-level keys with a warning, taking every absent key
// from the embedded default. Setting both forms is an error naming both.
func (fc *fileConfig) buildDuplication(cfg *Config) error {
	legacy := []legacyDupKey{
		{"dup_min_tokens", "min_tokens", fc.DupMinTokens != nil},
		{"dup_ignore_literal_only", "ignore_literal_only", fc.DupIgnoreLiteralOnly != nil},
		{"dup_fold_signs", "fold_signs", fc.DupFoldSigns != nil},
	}
	var used []string
	for _, k := range legacy {
		if k.set {
			used = append(used, k.old)
		}
	}
	d := fc.Duplication
	switch {
	case len(used) > 0 && d != nil:
		errs := make([]error, 0, len(used))
		for _, k := range legacy {
			if k.set {
				errs = append(errs, fmt.Errorf("%s and duplication.%s are both set; keep only duplication.%s", k.old, k.key, k.key))
			}
		}
		return errors.Join(errs...)
	case len(used) > 0:
		d = &fileDuplication{MinTokens: fc.DupMinTokens, IgnoreLiteralOnly: fc.DupIgnoreLiteralOnly, FoldSigns: fc.DupFoldSigns}
		cfg.Warnings = append(cfg.Warnings, strings.Join(used, ", ")+
			" deprecated: move under the duplication section as min_tokens, ignore_literal_only and fold_signs")
	case d == nil:
		d = &fileDuplication{}
	}
	if d.MinTokens == nil || d.IgnoreLiteralOnly == nil || d.FoldSigns == nil || d.SplitLiteralRuns == nil {
		def, err := defaultDuplication()
		if err != nil {
			return err
		}
		if d.MinTokens == nil {
			d.MinTokens = def.MinTokens
		}
		if d.IgnoreLiteralOnly == nil {
			d.IgnoreLiteralOnly = def.IgnoreLiteralOnly
		}
		if d.FoldSigns == nil {
			d.FoldSigns = def.FoldSigns
		}
		if d.SplitLiteralRuns == nil {
			d.SplitLiteralRuns = def.SplitLiteralRuns
		}
	}
	cfg.Duplication = Duplication{
		MinTokens:         *d.MinTokens,
		IgnoreLiteralOnly: *d.IgnoreLiteralOnly,
		FoldSigns:         *d.FoldSigns,
		// An embedded default without split_literal_runs leaves it off.
		SplitLiteralRuns: d.SplitLiteralRuns != nil && *d.SplitLiteralRuns,
	}
	return nil
}

// defaultDuplication decodes the duplication section of the embedded
// default, so the defaults are stated once, in default.yaml. The section
// must set every key but split_literal_runs, which is off when absent.
func defaultDuplication() (*fileDuplication, error) {
	var fc fileConfig
	if err := yaml.Unmarshal([]byte(defaultYAML), &fc); err != nil {
		return nil, fmt.Errorf("embedded default: %w", err)
	}
	d := fc.Duplication
	if d == nil || d.MinTokens == nil || d.IgnoreLiteralOnly == nil || d.FoldSigns == nil {
		return nil, errors.New("embedded default: duplication section is incomplete")
	}
	return d, nil
}
