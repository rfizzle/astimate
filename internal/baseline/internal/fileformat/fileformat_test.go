package fileformat

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/inventory"
)

// TestRoundTrip encodes every field a file records and decodes it back.
func TestRoundTrip(t *testing.T) {
	t.Parallel()

	cross := 2
	in := File{
		Ref:         "abc",
		GeneratedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		ModulePath:  "example.com/m",
		Tokenizer:   "o200k",
		Packages: map[string]metrics.RawMetrics{
			"example.com/m/a":   {Files: 2, SLOC: 40},
			"example.com/m/b":   {Files: 1},
			metrics.ModuleRowID: {DupBlocksCrossPkg: &cross},
		},
		Functions: map[string][]inventory.FunctionInfo{
			"example.com/m/a": {{Receiver: "T", Name: "Run", Fingerprint: 0xff, Signature: 0xabc, Cognitive: 4, Cyclomatic: 3}},
		},
		Named: map[string]Named{
			"example.com/m/a": {DupBlocks: []uint64{0x1, 0xfedcba9876543210}, UntestedExports: []string{"T.Run"},
				Exports: []inventory.Symbol{{Kind: "type", Name: "T"}, {Kind: "func", Receiver: "T", Name: "Run"}}},
		},
		Dirs: map[string]string{"example.com/m/a": "a", "example.com/m/b": "b"},
	}
	data, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for _, want := range []string{`"<module>": {`, `"fingerprint": "00000000000000ff"`, `"0000000000000001"`} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("encoded file lacks %s:\n%s", want, data)
		}
	}
	out, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Errorf("Decode(Encode(f)) = %+v, want %+v", out, in)
	}
}

// TestDecodeLegacy reads the layouts of files written before a field
// existed: no package_rows, the module row under "module", no functions or
// details.
func TestDecodeLegacy(t *testing.T) {
	t.Parallel()

	const head = `{"ref": "abc", "generated_at": "2026-01-01T00:00:00Z", "module_path": "example.com/m", "packages": {`
	tests := []struct {
		name     string
		body     string
		migrated bool
		byPath   bool
		dirs     map[string]string
	}{
		{name: "old module row", body: head + `"example.com/m/a": {"files": 1}, "module": {"dup_blocks_cross_pkg": 2}}}`, migrated: true, byPath: true},
		{name: "package named module", body: head + `"module": {"files": 1}}}`, byPath: true},
		{name: "module row only", body: head + `"<module>": {}}}`},
		{
			name: "package rows", body: head + `"example.com/m/a": {}}, "package_rows": {"example.com/m/a": {"package_path": "x"}}}`,
			dirs: map[string]string{"example.com/m/a": "x"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f, err := Decode([]byte(tt.body))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if f.MigratedModuleRow != tt.migrated || f.KeyedByImportPath != tt.byPath || !reflect.DeepEqual(f.Dirs, tt.dirs) {
				t.Errorf("Decode = migrated %v, by import path %v, dirs %v; want %v, %v, %v",
					f.MigratedModuleRow, f.KeyedByImportPath, f.Dirs, tt.migrated, tt.byPath, tt.dirs)
			}
			if f.Functions != nil || f.Named != nil || f.Tokenizer != "" {
				t.Errorf("Decode = functions %v, named %v, tokenizer %q; want none recorded", f.Functions, f.Named, f.Tokenizer)
			}
			if _, ok := f.Packages[metrics.ModuleRowID]; ok != (tt.migrated || tt.name == "module row only") {
				t.Errorf("packages = %v; module row present %v", f.Packages, ok)
			}
		})
	}
}

// TestDecodeErrors rejects malformed JSON and malformed hexadecimal fields.
func TestDecodeErrors(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"malformed":   `{not json`,
		"fingerprint": `{"packages": {}, "functions": {"a": [{"name": "F", "fingerprint": "xyz", "cognitive": 1}]}}`,
		"signature":   `{"packages": {}, "functions": {"a": [{"name": "F", "fingerprint": "00", "signature": "xyz", "cognitive": 1}]}}`,
		"anchor":      `{"packages": {"a": {}}, "package_rows": {"a": {"package_path": "a", "dup_blocks": ["xyz"]}}}`,
	} {
		if _, err := Decode([]byte(body)); err == nil {
			t.Errorf("%s: Decode accepted %s", name, body)
		}
	}
}
