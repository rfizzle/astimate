package report

import (
	"encoding/json"
	"fmt"
	"io"
	"math"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// Report is the per-package report of SPEC.md 10.2. Field names and JSON
// tags match the schema exactly. Violations, Warnings and Passed are omitted
// when no gate ran and present, the arrays possibly empty, when one did;
// Baseline is omitted when the package had no baseline.
type Report struct {
	// Language is the extractor's language identifier, for example "go".
	Language string `json:"language"`
	// PackagePath is the package directory relative to the module root, in
	// slash form; "." is the module root itself.
	PackagePath string `json:"package_path"`
	// ModulePath is the module's import path.
	ModulePath string `json:"module_path"`
	// Rebuild is the rebuild estimate (SPEC.md section 7).
	Rebuild Rebuild `json:"rebuild"`
	// Suggestions are the driver suggestions of SPEC.md 7.4; never null.
	Suggestions []string `json:"suggestions"`
	// Metrics is every field of SPEC.md section 6; v1 fields not computed
	// are null.
	Metrics metrics.RawMetrics `json:"metrics"`
	// Baseline is the metrics the package was compared against, if any.
	Baseline *Baseline `json:"baseline,omitempty"`
	// Violations are the failed gate rules: nil (omitted) when no gate ran,
	// non-nil and possibly empty (an empty array) when one did.
	Violations []Finding `json:"violations,omitzero"`
	// Warnings are the non-failing capacity findings, nil or non-nil as for
	// Violations.
	Warnings []Finding `json:"warnings,omitzero"`
	// Passed is the gate verdict; nil when no gate ran.
	Passed *bool `json:"passed,omitempty"`
	// AstimateVersion is the version of the binary that produced the report.
	AstimateVersion string `json:"astimate_version"`
	// ConfigVersion is the config_version of the configuration used.
	ConfigVersion string `json:"config_version"`
}

// Rebuild is the rebuild block of the report. Values are rounded for
// display: agent_passes and human_days to one decimal, tokens to integers.
type Rebuild struct {
	// AgentPasses is score.Rebuild.AgentPassesRounded.
	AgentPasses float64 `json:"agent_passes"`
	// RebuildTokens is rebuild_tokens rounded to an integer.
	RebuildTokens int `json:"rebuild_tokens"`
	// HumanDays is the human estimate rounded to one decimal.
	HumanDays float64 `json:"human_days"`
	// Tier is the tier of the unrounded agent_passes.
	Tier score.Tier `json:"tier"`
	// Calibrated reports whether the rebuild parameters were measured.
	Calibrated bool `json:"calibrated"`
	// Drivers are the largest terms of rebuild_tokens; never null.
	Drivers []Driver `json:"drivers"`
}

// Driver is one entry of rebuild.drivers.
type Driver struct {
	// Term is the term name, one of the score.Term* constants.
	Term string `json:"term"`
	// Tokens is the term's contribution, rounded to an integer.
	Tokens int `json:"tokens"`
	// Detail is the metric values the term was computed from.
	Detail string `json:"detail"`
}

// Baseline is the baseline block: the ref the metrics were taken at and
// the tokenizer they were counted with.
type Baseline struct {
	// Ref names the baseline, for example a commit hash.
	Ref string `json:"ref"`
	// Metrics is the package's metrics at Ref.
	Metrics metrics.RawMetrics `json:"metrics"`
	// Tokenizer is the tokenizer the baseline counted tokens with; empty
	// when unknown.
	Tokenizer string `json:"tokenizer,omitempty"`
	// TokensComparable reports whether Tokenizer is the check's own, so
	// that token deltas against the baseline mean something. A baseline
	// file written with another tokenizer leaves it false; the gate still
	// runs, since capacity rules are absolute.
	TokensComparable bool `json:"tokens_comparable"`
}

// Finding is one gate violation or warning.
type Finding struct {
	// Metric is the RawMetrics JSON field name.
	Metric string `json:"metric"`
	// Base is the baseline value; nil when there is none.
	Base *float64 `json:"base,omitempty"`
	// Head is the value at head.
	Head float64 `json:"head"`
	// Limit is the rule that was broken or approached.
	Limit string `json:"limit"`
	// Suggestion is the fix sentence.
	Suggestion string `json:"suggestion"`
	// File and Line locate the finding, for renderers that annotate the
	// line that caused it, such as a dup_blocks finding on a duplicate
	// block or the module row's dup_blocks_cross_pkg finding on the first
	// occurrence of the first shared block: File is relative to the module
	// root in slash form and Line is 1-based, 0 when only the file is
	// known. Empty when the finding has no location; a package finding is
	// then located by its package path. Neither is part of the SPEC.md
	// 10.2 schema.
	File string `json:"-"`
	Line int    `json:"-"`
}

// Input is everything Build composes a report from.
type Input struct {
	// Language is the extractor's language identifier.
	Language string
	// PackagePath is the package directory relative to the module root.
	PackagePath string
	// ModulePath is the module's import path.
	ModulePath string
	// Metrics is the package's extracted metrics.
	Metrics metrics.RawMetrics
	// Params are the rebuild parameters the estimate uses.
	Params score.RebuildParams
	// Names supplies identifiers for suggestions; the zero value is valid.
	Names score.Names
	// ConfigVersion is the config_version of the configuration used.
	ConfigVersion string
	// AstimateVersion is the version of the running binary.
	AstimateVersion string
}

// Build estimates the rebuild effort of in.Metrics under in.Params and
// composes the report, with no baseline and no gate result.
func Build(in *Input) Report {
	est := score.Estimate(in.Metrics, in.Params)
	ds := score.Drivers(est)
	drivers := make([]Driver, 0, len(ds))
	for _, d := range ds {
		drivers = append(drivers, Driver{Term: d.Term, Tokens: roundInt(d.Tokens), Detail: d.Detail})
	}
	suggestions := score.DriverSuggestions(est, in.Metrics, in.Names)
	if suggestions == nil {
		suggestions = []string{}
	}
	return Report{
		Language:    in.Language,
		PackagePath: in.PackagePath,
		ModulePath:  in.ModulePath,
		Rebuild: Rebuild{
			AgentPasses:   est.AgentPassesRounded(),
			RebuildTokens: roundInt(est.RebuildTokens),
			HumanDays:     round1(est.HumanDays),
			Tier:          score.TierOf(est.AgentPasses, in.Params.Tiers),
			Calibrated:    score.Calibrated(in.ConfigVersion),
			Drivers:       drivers,
		},
		Suggestions:     suggestions,
		Metrics:         in.Metrics,
		AstimateVersion: in.AstimateVersion,
		ConfigVersion:   in.ConfigVersion,
	}
}

// WriteJSON writes r to w as indented JSON followed by a newline.
func WriteJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("writing json report: %w", err)
	}
	return nil
}

func roundInt(v float64) int { return int(math.Round(v)) }

func round1(v float64) float64 { return math.Round(v*10) / 10 }
