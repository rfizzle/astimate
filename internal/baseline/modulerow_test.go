package baseline

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// TestFileModuleNamedModule round-trips the baseline of a module whose path
// is "module": its root package's row and the module row are kept apart,
// and the reserved key is stored as written, not HTML-escaped.
func TestFileModuleNamedModule(t *testing.T) {
	t.Parallel()

	cross := 1
	pkgs := map[string]metrics.RawMetrics{
		"module":            {Files: 1, SLOC: 3},
		"module/sub":        {Files: 2, SLOC: 9},
		metrics.ModuleRowID: {DupBlocksCrossPkg: &cross},
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := Write(path, "abc", "module", "est", pkgs); err != nil {
		t.Fatalf("Write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"<module>": {`, `"module": {`} {
		if !bytes.Contains(data, []byte(key)) {
			t.Errorf("file lacks the key %s:\n%s", key, data)
		}
	}

	b, err := FromFile(path)
	if err != nil {
		t.Fatalf("FromFile: %v", err)
	}
	for pkg, want := range pkgs {
		if got, ok := b.Metrics(pkg); !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("Metrics(%q) = %+v (present %v), want %+v", pkg, got, ok, want)
		}
	}
	if MigratedModuleRow(b) {
		t.Error("MigratedModuleRow = true for a file with the reserved key")
	}
}

// TestFileLegacyModuleKey reads files that use the key "module": as the
// module row when it has the row's shape and the file has no row under the
// reserved key, and as the package of that import path otherwise.
func TestFileLegacyModuleKey(t *testing.T) {
	t.Parallel()

	const head = `{"ref": "abc", "generated_at": "2026-01-01T00:00:00Z", "module_path": "example.com/m", "packages": {`
	tests := []struct {
		name     string
		packages string
		migrated bool
		// row and pkg are the rows wanted under metrics.ModuleRowID and
		// "module"; nil wants none.
		row, pkg *metrics.RawMetrics
	}{
		{
			name:     "old module row",
			packages: `"example.com/m/a": {"files": 1}, "module": {"dup_blocks_cross_pkg": 2}`,
			migrated: true,
			row:      &metrics.RawMetrics{DupBlocksCrossPkg: new(2)},
		},
		{
			name:     "package named module",
			packages: `"module": {"files": 1, "sloc": 4}`,
			pkg:      &metrics.RawMetrics{Files: 1, SLOC: 4},
		},
		{
			name:     "reserved key present",
			packages: `"module": {"dup_blocks_cross_pkg": 2}, "<module>": {"dup_blocks_cross_pkg": 3}`,
			row:      &metrics.RawMetrics{DupBlocksCrossPkg: new(3)},
			pkg:      &metrics.RawMetrics{DupBlocksCrossPkg: new(2)},
		},
		{
			name:     "no module row",
			packages: `"example.com/m/a": {"files": 1}`,
		},
	}
	dir := t.TempDir()
	for i, tt := range tests {
		path := filepath.Join(dir, "b"+strconv.Itoa(i)+".json")
		if err := os.WriteFile(path, []byte(head+tt.packages+"}}"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, err := FromFile(path)
			if err != nil {
				t.Fatalf("FromFile: %v", err)
			}
			if got := MigratedModuleRow(b); got != tt.migrated {
				t.Errorf("MigratedModuleRow = %v, want %v", got, tt.migrated)
			}
			for key, want := range map[string]*metrics.RawMetrics{metrics.ModuleRowID: tt.row, "module": tt.pkg} {
				got, ok := b.Metrics(key)
				switch {
				case want == nil && ok:
					t.Errorf("Metrics(%q) = %+v, want none", key, got)
				case want != nil && (!ok || !reflect.DeepEqual(got, *want)):
					t.Errorf("Metrics(%q) = %+v (present %v), want %+v", key, got, ok, *want)
				}
			}
		})
	}
}
