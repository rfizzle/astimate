package metrics

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

// v1Names lists the v1 fields, the ones that are null until computed.
func v1Names() []string {
	return []string{
		"dup_blocks_cross_pkg", "instability", "abstractness", "main_sequence_distance",
		"uses_cgo", "uses_reflect", "generated_files", "tokens_est_generated", "coverage_pct", "changed_func_cognitive_max",
	}
}

// fullMetrics returns a record with every field set to a distinct non-zero
// value so a dropped or swapped field shows up in comparisons.
func fullMetrics() RawMetrics {
	return RawMetrics{
		Files:                   1,
		SLOC:                    200,
		LargestFileSLOC:         150,
		TokensEst:               4,
		TokensEstWithTests:      5,
		InternalImports:         6,
		ExternalImports:         7,
		StdlibImports:           8,
		FanIn:                   9,
		FanInTests:              10,
		ExportedSymbols:         11,
		Globals:                 12,
		InitFuncs:               13,
		MaxNesting:              14,
		CognitiveTotal:          15,
		CognitiveP90:            16,
		FuncCount:               17,
		DupBlocks:               18,
		DuplicationPct:          19.5,
		TestFiles:               20,
		TestFuncs:               21,
		HasTests:                true,
		UntestedExports:         22,
		DupBlocksCrossPkg:       ptr(23),
		Instability:             ptr(0.25),
		Abstractness:            ptr(0.625),
		MainSequenceDistance:    ptr(0.125),
		UsesCgo:                 ptr(true),
		UsesReflect:             ptr(false),
		GeneratedFiles:          ptr(24),
		TokensEstGenerated:      ptr(26),
		CoveragePct:             ptr(87.5),
		ChangedFuncCognitiveMax: ptr(25),
	}
}

// jsonKeys returns the top-level object keys of data in encoding order.
func jsonKeys(t *testing.T, data []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	if _, err := dec.Token(); err != nil {
		t.Fatalf("reading object start: %v", err)
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("reading key: %v", err)
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatalf("reading value: %v", err)
		}
	}
	return keys
}

func TestRawMetricsJSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   RawMetrics
	}{
		{name: "all fields set", in: fullMetrics()},
		{name: "zero value", in: RawMetrics{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if got, want := jsonKeys(t, data), MetricNames(); !slices.Equal(got, want) {
				t.Errorf("JSON keys = %v, want %v", got, want)
			}
			var out RawMetrics
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(out, tt.in) {
				t.Errorf("round trip mismatch:\n got %+v\nwant %+v", out, tt.in)
			}
		})
	}
}

func TestRawMetricsV1NullWhenNotComputed(t *testing.T) {
	data, err := json.Marshal(RawMetrics{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	v1 := v1Names()
	for _, name := range v1 {
		if got := string(fields[name]); got != "null" {
			t.Errorf("%s = %s, want null", name, got)
		}
	}
	for _, name := range MetricNames() {
		if slices.Contains(v1, name) {
			continue
		}
		if got := string(fields[name]); got == "null" {
			t.Errorf("v0 field %s serialized as null", name)
		}
	}
}

func TestRawMetricsValue(t *testing.T) {
	m := fullMetrics()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, name := range MetricNames() {
		t.Run(name, func(t *testing.T) {
			got, ok := m.Value(name)
			if !ok {
				t.Fatalf("Value(%q) not found", name)
			}
			want := fields[name]
			if b, isBool := want.(bool); isBool {
				want = boolValue(b)
			}
			if got != want {
				t.Errorf("Value(%q) = %v, want %v", name, got, want)
			}
			var zero RawMetrics
			_, ok = zero.Value(name)
			isV1 := slices.Contains(v1Names(), name)
			if ok == isV1 {
				t.Errorf("zero Value(%q) ok = %v, want %v", name, ok, !isV1)
			}
		})
	}
	if _, ok := m.Value("no_such_metric"); ok {
		t.Error("Value of unknown name reported ok")
	}
}

func TestRawMetricsValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*RawMetrics)
		wantErr string
	}{
		{name: "valid full record", mutate: func(*RawMetrics) {}},
		{name: "valid zero record", mutate: func(m *RawMetrics) { *m = RawMetrics{} }},
		{name: "negative files", mutate: func(m *RawMetrics) { m.Files = -1 }, wantErr: "files is negative"},
		{name: "negative globals", mutate: func(m *RawMetrics) { m.Globals = -2 }, wantErr: "globals is negative"},
		{name: "negative untested exports", mutate: func(m *RawMetrics) { m.UntestedExports = -1 }, wantErr: "untested_exports is negative"},
		{name: "negative v1 count", mutate: func(m *RawMetrics) { m.GeneratedFiles = ptr(-1) }, wantErr: "generated_files is negative"},
		{name: "negative generated tokens", mutate: func(m *RawMetrics) { m.TokensEstGenerated = ptr(-1) }, wantErr: "tokens_est_generated is negative"},
		{name: "generated tokens without generated files", mutate: func(m *RawMetrics) { m.GeneratedFiles = ptr(0) }, wantErr: "tokens_est_generated 26 with generated_files 0"},
		{name: "no generated files and no generated tokens", mutate: func(m *RawMetrics) { m.GeneratedFiles, m.TokensEstGenerated = ptr(0), ptr(0) }},
		{name: "generated tokens without a generated_files count", mutate: func(m *RawMetrics) { m.GeneratedFiles = nil }},
		{name: "negative changed func max", mutate: func(m *RawMetrics) { m.ChangedFuncCognitiveMax = ptr(-3) }, wantErr: "changed_func_cognitive_max is negative"},
		{name: "duplication above 100", mutate: func(m *RawMetrics) { m.DuplicationPct = 100.1 }, wantErr: "duplication_pct"},
		{name: "duplication negative", mutate: func(m *RawMetrics) { m.DuplicationPct = -0.1 }, wantErr: "duplication_pct"},
		{name: "duplication NaN", mutate: func(m *RawMetrics) { m.DuplicationPct = math.NaN() }, wantErr: "duplication_pct"},
		{name: "duplication at 100", mutate: func(m *RawMetrics) { m.DuplicationPct = 100 }},
		{name: "coverage above 100", mutate: func(m *RawMetrics) { m.CoveragePct = ptr(101.0) }, wantErr: "coverage_pct"},
		{name: "instability above 1", mutate: func(m *RawMetrics) { m.Instability = ptr(1.01) }, wantErr: "instability"},
		{name: "abstractness negative", mutate: func(m *RawMetrics) { m.Abstractness = ptr(-0.5) }, wantErr: "abstractness"},
		{name: "distance NaN", mutate: func(m *RawMetrics) { m.MainSequenceDistance = ptr(math.NaN()) }, wantErr: "main_sequence_distance"},
		{name: "ratios at bounds", mutate: func(m *RawMetrics) {
			m.Instability, m.Abstractness, m.MainSequenceDistance = ptr(1.0), ptr(0.0), ptr(1.0)
			m.CoveragePct = ptr(0.0)
		}},
		{name: "largest file above sloc", mutate: func(m *RawMetrics) { m.LargestFileSLOC = m.SLOC + 1 }, wantErr: "largest_file_sloc"},
		{name: "tokens with tests below tokens", mutate: func(m *RawMetrics) { m.TokensEstWithTests = m.TokensEst - 1 }, wantErr: "tokens_est_with_tests"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := fullMetrics()
			tt.mutate(&m)
			err := m.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tt.wantErr)
			}
			if !errors.Is(err, ErrInvalidMetrics) {
				t.Errorf("Validate() = %v, want wrapping ErrInvalidMetrics", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() = %v, want mention of %q", err, tt.wantErr)
			}
		})
	}
}

func TestRawMetricsValidateReportsAllProblems(t *testing.T) {
	m := RawMetrics{Files: -1, SLOC: -1, DuplicationPct: 200}
	err := m.Validate()
	for _, want := range []string{"files is negative", "sloc is negative", "duplication_pct"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() = %v, want mention of %q", err, want)
		}
	}
}
