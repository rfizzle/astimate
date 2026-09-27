package metricstest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
)

// LoadGolden reads <dir>/<pkg>.json, the hand-verified metrics of pkg. The
// file uses the RawMetrics JSON encoding; every v0 field must be present,
// v1 fields may be omitted or null, and unknown fields are rejected so a
// misspelled name cannot silently compare as zero. TestExtractor calls it
// for each package; call it directly only when a test needs a golden record
// outside the suite, for example as input to a Fake.
func LoadGolden(dir, pkg string) (metrics.RawMetrics, error) {
	path := goldenPath(dir, pkg)
	data, err := os.ReadFile(path)
	if err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("loading golden: %w", err)
	}

	var m metrics.RawMetrics
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("decoding golden %s: %w", path, err)
	}

	var present map[string]json.RawMessage
	if err := json.Unmarshal(data, &present); err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("decoding golden %s: %w", path, err)
	}
	var missing []string
	for _, name := range v0Names() {
		if raw, ok := present[name]; !ok || string(raw) == "null" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return metrics.RawMetrics{}, fmt.Errorf("golden %s lacks v0 fields: %s", path, strings.Join(missing, ", "))
	}
	return m, nil
}

func goldenPath(dir, pkg string) string {
	return filepath.Join(dir, filepath.FromSlash(pkg)+".json")
}

// v0Names returns the metric names that are always computed. A v0 field is
// one whose Value is reported on the zero record; v1 fields are pointers and
// report nothing until set. Deriving the split from Value means a field added
// to RawMetrics is compared without touching this package.
func v0Names() []string {
	var zero metrics.RawMetrics
	names := metrics.MetricNames()
	v0 := names[:0]
	for _, name := range names {
		if _, ok := zero.Value(name); ok {
			v0 = append(v0, name)
		}
	}
	return v0
}

// fieldDiff is one metric whose extracted value differs from its golden.
type fieldDiff struct {
	field, golden, got string
}

// diff compares every metric the golden sets: all v0 fields, and each v1
// field whose golden value is non-null.
func diff(golden, got *metrics.RawMetrics) []fieldDiff {
	var out []fieldDiff
	for _, name := range metrics.MetricNames() {
		want, set := golden.Value(name)
		if !set {
			continue
		}
		have, computed := got.Value(name)
		switch {
		case !computed:
			out = append(out, fieldDiff{name, formatValue(want), "null"})
		case have != want:
			out = append(out, fieldDiff{name, formatValue(want), formatValue(have)})
		}
	}
	return out
}

func formatValue(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// writeGolden rewrites <dir>/<pkg>.json from m in MetricNames order with a
// two-space indent, omitting v1 fields that were not computed, which matches
// the layout of hand-written goldens.
func writeGolden(dir, pkg string, m metrics.RawMetrics) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encoding golden: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("encoding golden: %w", err)
	}

	var b strings.Builder
	b.WriteString("{\n")
	first := true
	for _, name := range metrics.MetricNames() {
		raw, ok := fields[name]
		if !ok || string(raw) == "null" {
			continue
		}
		if !first {
			b.WriteString(",\n")
		}
		first = false
		b.WriteString(`  "`)
		b.WriteString(name)
		b.WriteString(`": `)
		b.Write(raw)
	}
	b.WriteString("\n}\n")

	path := goldenPath(dir, pkg)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("writing golden: %w", err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("writing golden: %w", err)
	}
	return nil
}
