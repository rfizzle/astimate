package metrics

import "strings"

// numFields is the number of RawMetrics fields, the rows of fieldTable.
const numFields = 35

// field is one row of SPEC.md section 6's metric table: what MetricNames,
// Value, Delta, Validate and ModuleWide know about a RawMetrics field. Its
// columns are literals, so the table is data, not code. The explain_metric
// texts and the default-gate flags of each field live in package
// metrics/explain, keyed by the same name.
type field struct {
	// name is the JSON field name.
	name string
	// release is "v0" for a field every extractor computes and "v1" for
	// one that is null until computed; a v1 field is a pointer.
	release string
	// upper is the inclusive bound Validate holds a float field to, 100
	// for a percentage and 1 for a ratio; 0 for a count or a flag, which
	// Validate holds to non-negative and not at all.
	upper float64
	// flags holds "module-wide" when the module row carries the metric
	// (ModuleWide), space-separated.
	flags string
}

// has reports whether f's flags hold flag.
func (f field) has(flag string) bool {
	for w := range strings.FieldsSeq(f.flags) {
		if w == flag {
			return true
		}
	}
	return false
}

// fieldTable returns the metric table in SPEC.md section 6 order, the
// order of the RawMetrics fields and of slots.
func fieldTable() [numFields]field {
	return [numFields]field{
		{"files", "v0", 0, ""},
		{"sloc", "v0", 0, ""},
		{"largest_file_sloc", "v0", 0, ""},
		{"tokens_est", "v0", 0, ""},
		{"tokens_est_with_tests", "v0", 0, ""},
		{"internal_imports", "v0", 0, ""},
		{"external_imports", "v0", 0, ""},
		{"stdlib_imports", "v0", 0, ""},
		{"fan_in", "v0", 0, ""},
		{"fan_in_tests", "v0", 0, ""},
		{"exported_symbols", "v0", 0, ""},
		{"globals", "v0", 0, ""},
		{"init_funcs", "v0", 0, ""},
		{"max_nesting", "v0", 0, ""},
		{"cognitive_total", "v0", 0, ""},
		{"cognitive_p90", "v0", 0, ""},
		{"func_count", "v0", 0, ""},
		{"dup_blocks", "v0", 0, ""},
		{"dup_blocks_code", "v1", 0, ""},
		{"duplication_pct", "v0", 100, ""},
		{"duplicated_lines", "v1", 0, ""},
		{"test_files", "v0", 0, ""},
		{"test_funcs", "v0", 0, ""},
		{"has_tests", "v0", 0, ""},
		{"untested_exports", "v0", 0, ""},
		{"dup_blocks_cross_pkg", "v1", 0, "module-wide"},
		{"instability", "v1", 1, ""},
		{"abstractness", "v1", 1, ""},
		{"main_sequence_distance", "v1", 1, ""},
		{"uses_cgo", "v1", 0, ""},
		{"uses_reflect", "v1", 0, ""},
		{"generated_files", "v1", 0, ""},
		{"tokens_est_generated", "v1", 0, ""},
		{"coverage_pct", "v1", 100, ""},
		{"changed_func_cognitive_max", "v1", 0, ""},
	}
}

// fieldIndex returns the row of fieldTable named name, or -1.
func fieldIndex(name string) int {
	t := fieldTable()
	for i := range t {
		if t[i].name == name {
			return i
		}
	}
	return -1
}

// slots returns a pointer to every field of m in fieldTable order: an
// *int, *float64 or *bool for a v0 field, and a pointer to the pointer
// field for a v1 one.
func (m *RawMetrics) slots() [numFields]any {
	return [numFields]any{
		&m.Files, &m.SLOC, &m.LargestFileSLOC, &m.TokensEst, &m.TokensEstWithTests, &m.InternalImports,
		&m.ExternalImports, &m.StdlibImports, &m.FanIn, &m.FanInTests, &m.ExportedSymbols, &m.Globals,
		&m.InitFuncs, &m.MaxNesting, &m.CognitiveTotal, &m.CognitiveP90, &m.FuncCount, &m.DupBlocks,
		&m.DupBlocksCode, &m.DuplicationPct, &m.DuplicatedLines, &m.TestFiles, &m.TestFuncs, &m.HasTests, &m.UntestedExports,
		&m.DupBlocksCrossPkg, &m.Instability, &m.Abstractness, &m.MainSequenceDistance, &m.UsesCgo,
		&m.UsesReflect, &m.GeneratedFiles, &m.TokensEstGenerated, &m.CoveragePct, &m.ChangedFuncCognitiveMax,
	}
}

// valueKind is how a field's value is stored.
type valueKind uint8

const (
	kindInt valueKind = iota
	kindFloat
	kindBool
)

// read returns the value a slot points at as a float64, bools as 0 or 1,
// with its kind; ok is false for a nil v1 field.
func read(slot any) (v float64, ok bool, k valueKind) {
	switch p := slot.(type) {
	case *int:
		return float64(*p), true, kindInt
	case *float64:
		return *p, true, kindFloat
	case *bool:
		return boolValue(*p), true, kindBool
	case **int:
		n, ok := deref(*p)
		return float64(n), ok, kindInt
	case **float64:
		f, ok := deref(*p)
		return f, ok, kindFloat
	case **bool:
		b, ok := deref(*p)
		return boolValue(b), ok, kindBool
	}
	return 0, false, kindInt
}

// deref returns *p, and false with the zero value when p is nil.
func deref[T any](p *T) (T, bool) {
	if p == nil {
		var zero T
		return zero, false
	}
	return *p, true
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
