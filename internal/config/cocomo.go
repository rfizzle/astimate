package config

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/rfizzle/astimate/internal/score"
)

// applyCocomo sets on c what m, a decoded rebuild.cocomo section (SPEC.md
// 7.3), sets: model, unfm, and the rating of each driver score.CocomoKeys
// names.
// A key m lacks keeps c's value. It reports each other key as an error
// prefixed with prefix; rating names are checked by Validate.
func applyCocomo(m map[string]string, c *score.Cocomo, prefix string) error {
	var errs []error
	for _, k := range slices.Sorted(maps.Keys(m)) {
		switch {
		case k == "model":
			c.Model = m[k]
		case k == "unfm":
			c.UNFM = m[k]
		case !c.Set(k, score.Rating(m[k])):
			errs = append(errs, fmt.Errorf("%scocomo.%s: unknown key; RELY is nominal and CPLX is rated per package", prefix, k))
		}
	}
	return errors.Join(errs...)
}

// buildCocomo fills cfg.Rebuild.Cocomo from the embedded default's
// rebuild.cocomo, then from the file's, so a key absent from the file
// takes the default's rating.
func (fc *fileConfig) buildCocomo(cfg *Config) error {
	var def fileConfig
	if err := yaml.Unmarshal([]byte(defaultYAML), &def); err != nil {
		return fmt.Errorf("embedded default: %w", err)
	}
	if def.Rebuild == nil || len(def.Rebuild.Cocomo) == 0 {
		return errors.New("embedded default: rebuild.cocomo section is missing")
	}
	if err := applyCocomo(def.Rebuild.Cocomo, &cfg.Rebuild.Cocomo, "embedded default: rebuild."); err != nil {
		return err
	}
	if fc.Rebuild == nil {
		return nil
	}
	return applyCocomo(fc.Rebuild.Cocomo, &cfg.Rebuild.Cocomo, "rebuild.")
}
