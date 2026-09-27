package config

import (
	"errors"
	"fmt"
	"slices"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/score"
)

// Effective is the configuration one language is evaluated with: the top
// level with that language's override, if any, applied.
type Effective struct {
	// Version is config_version, suffixed with "+<language>" when the
	// language has an override, so a report shows which override applied.
	Version string
	// Rebuild holds the rebuild-estimate parameters.
	Rebuild score.RebuildParams
	// Thresholds holds the gate rules: the top-level rules in file order,
	// each replaced by the override's rules on the same metric or dropped
	// when the override disables it, then the override's rules on metrics
	// the top level does not gate. Callers must not modify it.
	Thresholds []gate.Threshold
}

// fileLanguage mirrors one entry of the languages section.
type fileLanguage struct {
	Rebuild    *fileRebuild    `yaml:"rebuild"`
	Thresholds []fileThreshold `yaml:"thresholds"`
}

// languageOverride is one language's parsed override.
type languageOverride struct {
	// rebuild holds the parameters the override sets; nil fields inherit.
	rebuild *fileRebuild
	// rules replace the top-level rules on the same metric, or add one.
	rules []gate.Threshold
	// ruleIndex is each rule's index in the language's thresholds list,
	// for error messages.
	ruleIndex []int
	// disabled lists the metrics whose top-level rules the language drops.
	disabled []string
}

// empty reports an override that changes nothing.
func (o languageOverride) empty() bool {
	if len(o.rules) > 0 || len(o.disabled) > 0 {
		return false
	}
	if o.rebuild == nil {
		return true
	}
	var p score.RebuildParams
	for _, f := range o.rebuild.fields(&p) {
		if f.src != nil {
			return false
		}
	}
	return true
}

// knownLanguage reports the language ids of the shipped extractors. An
// override for any other id is kept, with a warning, since it can never
// apply.
func knownLanguage(id string) bool {
	switch id {
	case "go", "typescript":
		return true
	}
	return false
}

// ForLanguage returns the configuration the language lang is evaluated
// with. Without an override for lang it is the top level unchanged.
func (c *Config) ForLanguage(lang string) Effective {
	o, ok := c.languages[lang]
	if !ok || o.empty() {
		return Effective{Version: c.Version, Rebuild: c.Rebuild, Thresholds: c.Thresholds}
	}
	eff := Effective{Version: c.Version + "+" + lang, Rebuild: c.Rebuild}
	if o.rebuild != nil {
		for _, f := range o.rebuild.fields(&eff.Rebuild) {
			if f.src != nil {
				*f.dst = *f.src
			}
		}
	}
	eff.Thresholds = mergeRules(c.Thresholds, o)
	return eff
}

// mergeRules applies o's rules and disabled metrics to top, keeping the
// position of the first top-level rule on each replaced metric.
func mergeRules(top []gate.Threshold, o languageOverride) []gate.Threshold {
	if len(o.rules) == 0 && len(o.disabled) == 0 {
		return top
	}
	byMetric := make(map[string][]gate.Threshold, len(o.rules))
	for _, r := range o.rules {
		byMetric[r.Metric] = append(byMetric[r.Metric], r)
	}
	out := make([]gate.Threshold, 0, len(top)+len(o.rules))
	placed := make(map[string]bool, len(byMetric))
	for _, r := range top {
		if slices.Contains(o.disabled, r.Metric) {
			continue
		}
		rs, ok := byMetric[r.Metric]
		if !ok {
			out = append(out, r)
			continue
		}
		if !placed[r.Metric] {
			out = append(out, rs...)
			placed[r.Metric] = true
		}
	}
	for _, r := range o.rules {
		if !placed[r.Metric] {
			out = append(out, r)
		}
	}
	return out
}

// buildLanguages parses the languages section into cfg, warning on an
// unknown language id and on disabling a metric the top level does not
// gate. A disabled rule that sets anything but metric, or a metric both
// disabled and overridden, is an error naming the language. Range checks
// are left to Validate.
func (fc *fileConfig) buildLanguages(cfg *Config) error {
	if len(fc.Languages) == 0 {
		return nil
	}
	ids := make([]string, 0, len(fc.Languages))
	for id := range fc.Languages {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	cfg.languages = make(map[string]languageOverride, len(ids))
	var errs []error
	for _, id := range ids {
		if !knownLanguage(id) {
			cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("languages.%s: unknown language; known: go, typescript", id))
		}
		fl := fc.Languages[id]
		if fl == nil {
			cfg.languages[id] = languageOverride{}
			continue
		}
		o := languageOverride{rebuild: fl.Rebuild}
		for i, ft := range fl.Thresholds {
			prefix := fmt.Sprintf("languages.%s.thresholds[%d] %q", id, i, ft.Metric)
			if ft.Disabled {
				if err := disabledShape(ft); err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", prefix, err))
					continue
				}
				o.disabled = append(o.disabled, ft.Metric)
				if !slices.ContainsFunc(cfg.Thresholds, func(t gate.Threshold) bool { return t.Metric == ft.Metric }) {
					cfg.Warnings = append(cfg.Warnings, prefix+": disables a metric the top-level thresholds do not gate")
				}
				continue
			}
			t, err := ft.build(prefix)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			o.rules = append(o.rules, t)
			o.ruleIndex = append(o.ruleIndex, i)
		}
		for _, m := range o.disabled {
			if slices.ContainsFunc(o.rules, func(t gate.Threshold) bool { return t.Metric == m }) {
				errs = append(errs, fmt.Errorf("languages.%s.thresholds: %q is both disabled and overridden", id, m))
			}
		}
		cfg.languages[id] = o
	}
	return errors.Join(errs...)
}

// disabledShape checks that a disabled rule names its metric and sets
// nothing else.
func disabledShape(ft fileThreshold) error {
	if ft.Metric == "" {
		return errors.New("metric is required")
	}
	if ft.Kind != "" || ft.Max != nil || ft.MaxDelta != nil || ft.RatchetFromZero ||
		ft.WarnAt != nil || ft.Require != nil || ft.When != "" {
		return errors.New("a disabled rule sets only metric and disabled")
	}
	return nil
}

// validateLanguages checks each override on its own: the language's
// effective rebuild parameters, its rules and its disabled metrics, each
// error naming the language.
func (c *Config) validateLanguages(isKnown func(string) bool) []error {
	ids := make([]string, 0, len(c.languages))
	for id := range c.languages {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var errs []error
	for _, id := range ids {
		o := c.languages[id]
		if o.rebuild != nil {
			if err := c.ForLanguage(id).Rebuild.Validate(); err != nil {
				errs = append(errs, fmt.Errorf("languages.%s.rebuild: %w", id, err))
			}
		}
		for j, t := range o.rules {
			if err := validateRule(t, isKnown); err != nil {
				errs = append(errs, fmt.Errorf("languages.%s.thresholds[%d]: %w", id, o.ruleIndex[j], err))
			}
		}
		for _, m := range o.disabled {
			if !isKnown(m) {
				errs = append(errs, fmt.Errorf("languages.%s.thresholds: disabled %q: unknown metric", id, m))
			}
		}
	}
	return errs
}
