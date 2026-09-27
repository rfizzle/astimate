package gate

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Kind classifies a threshold rule (SPEC.md section 8.1).
type Kind string

// Threshold kinds.
const (
	// Density rules ratchet on the change with max_delta and an optional max.
	Density Kind = "density"
	// Capacity rules carry an absolute max and a warn_at band.
	Capacity Kind = "capacity"
	// Requirement rules demand a boolean value, optionally guarded by when.
	Requirement Kind = "requirement"
)

// Condition is a guard of the form "<metric> > <value>".
type Condition struct {
	// Metric is the metric name on the left of the comparison.
	Metric string
	// Value is the number the metric must exceed.
	Value float64
}

// Threshold is one gate rule for one metric.
type Threshold struct {
	// Metric is the RawMetrics JSON field name the rule applies to.
	Metric string
	// Kind selects which limits apply.
	Kind Kind
	// Max is the absolute ceiling: optional for density, required for
	// capacity.
	Max *float64
	// MaxDelta is the largest permitted increase from baseline; density only.
	MaxDelta *float64
	// RatchetFromZero makes a density rule evaluate MaxDelta against zero for
	// a package with no baseline; otherwise only Max applies to it.
	RatchetFromZero bool
	// WarnAt is the fraction of Max above which a capacity rule warns; zero
	// for other kinds.
	WarnAt float64
	// Require is the boolean value a requirement rule demands.
	Require *bool
	// When optionally guards the rule; the rule applies only when it holds.
	When *Condition
}

// Validate checks the rule's shape for its kind and that every metric it
// names satisfies known. Each error names the rule's metric; all are joined
// with errors.Join.
func (t Threshold) Validate(known func(string) bool) error {
	var errs []error
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("threshold %q: "+format, append([]any{t.Metric}, args...)...))
	}
	switch {
	case t.Metric == "":
		add("metric is required")
	case !known(t.Metric):
		add("unknown metric")
	}
	switch t.Kind {
	case "":
		add("kind is required")
	case Density:
		if t.MaxDelta == nil {
			add("density rule needs max_delta")
		}
		if t.WarnAt != 0 {
			add("warn_at applies only to capacity rules")
		}
		if t.Require != nil {
			add("require applies only to requirement rules")
		}
	case Capacity:
		if t.Max == nil {
			add("capacity rule needs max")
		} else if *t.Max <= 0 {
			add("max must be > 0, got %v", *t.Max)
		}
		if t.MaxDelta != nil {
			add("capacity rule must not set max_delta")
		}
		if t.WarnAt <= 0 || t.WarnAt >= 1 {
			add("warn_at must be in (0, 1), got %v", t.WarnAt)
		}
		if t.Require != nil {
			add("require applies only to requirement rules")
		}
		if t.RatchetFromZero {
			add("ratchet_from_zero applies only to density rules")
		}
	case Requirement:
		if t.Require == nil {
			add("requirement rule needs require")
		}
		if t.Max != nil || t.MaxDelta != nil {
			add("requirement rule must not set max or max_delta")
		}
		if t.WarnAt != 0 {
			add("warn_at applies only to capacity rules")
		}
		if t.RatchetFromZero {
			add("ratchet_from_zero applies only to density rules")
		}
	default:
		add("unknown kind %q", t.Kind)
	}
	if t.When != nil && !known(t.When.Metric) {
		add("when: unknown metric %q", t.When.Metric)
	}
	return errors.Join(errs...)
}

// ParseCondition parses the fixed guard form "<metric> > <number>".
// Surrounding whitespace is tolerated; any other form is an error.
func ParseCondition(s string) (Condition, error) {
	left, right, ok := strings.Cut(s, ">")
	if !ok {
		return Condition{}, fmt.Errorf("condition %q: want \"<metric> > <number>\"", s)
	}
	metric := strings.TrimSpace(left)
	if metric == "" || strings.ContainsAny(metric, " \t\r\n<>=") {
		return Condition{}, fmt.Errorf("condition %q: invalid metric name %q", s, metric)
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(right), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return Condition{}, fmt.Errorf("condition %q: want a finite number after '>'", s)
	}
	return Condition{Metric: metric, Value: v}, nil
}
