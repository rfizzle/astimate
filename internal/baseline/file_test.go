package baseline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/metrics"
)

func TestFileRoundTrip(t *testing.T) {
	t.Parallel()

	ratio := 0.25
	cross := 3
	pkgs := map[string]metrics.RawMetrics{
		"example.com/m/a": {Files: 2, SLOC: 40, Globals: 1, DuplicationPct: 12.5, HasTests: true},
		"example.com/m/b": {Files: 1, SLOC: 7, Instability: &ratio},
		// The module row is written and read back like a package.
		metrics.ModuleRowID: {DupBlocksCrossPkg: &cross},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")

	before := time.Now().UTC().Truncate(time.Second)
	if err := Write(path, "abc123", "example.com/m", "est", pkgs); err != nil {
		t.Fatalf("Write: %v", err)
	}

	b, err := FromFile(path)
	if err != nil {
		t.Fatalf("FromFile: %v", err)
	}
	if b.Ref() != "abc123" {
		t.Errorf("Ref() = %q, want %q", b.Ref(), "abc123")
	}
	for pkg, want := range pkgs {
		got, ok := b.Metrics(pkg)
		if !ok {
			t.Errorf("Metrics(%q): not found", pkg)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Metrics(%q) = %+v, want %+v", pkg, got, want)
		}
	}
	if _, ok := b.Metrics("example.com/m/renamed"); ok {
		t.Error("Metrics of an unknown import path reported found")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading baseline: %v", err)
	}
	var raw struct {
		Ref         string                     `json:"ref"`
		GeneratedAt string                     `json:"generated_at"`
		ModulePath  string                     `json:"module_path"`
		Packages    map[string]json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decoding baseline: %v", err)
	}
	if raw.ModulePath != "example.com/m" || len(raw.Packages) != len(pkgs) {
		t.Errorf("module_path = %q with %d packages, want example.com/m with %d", raw.ModulePath, len(raw.Packages), len(pkgs))
	}
	if _, ok := raw.Packages[metrics.ModuleRowID]; !ok {
		t.Errorf("packages = %v, want the module row under %q", raw.Packages, metrics.ModuleRowID)
	}
	at, err := time.Parse(time.RFC3339, raw.GeneratedAt)
	if err != nil {
		t.Errorf("generated_at %q is not RFC 3339: %v", raw.GeneratedAt, err)
	} else if at.Before(before) || at.After(time.Now().Add(time.Second)) {
		t.Errorf("generated_at = %s, want about now", at)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat baseline: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o644 {
		t.Errorf("baseline mode = %o, want 644", perm)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("listing %s: %v", dir, err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries after Write, want only the baseline", len(entries))
	}
}

func TestWriteReplacesExistingFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := Write(path, "old", "example.com/m", "est", map[string]metrics.RawMetrics{"example.com/m/a": {Files: 1}}); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if err := Write(path, "new", "example.com/m", "est", nil); err != nil {
		t.Fatalf("second Write: %v", err)
	}
	b, err := FromFile(path)
	if err != nil {
		t.Fatalf("FromFile: %v", err)
	}
	if b.Ref() != "new" {
		t.Errorf("Ref() = %q, want %q", b.Ref(), "new")
	}
	if _, ok := b.Metrics("example.com/m/a"); ok {
		t.Error("package from the replaced file is still present")
	}
}

func TestFileErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seeding bad file: %v", err)
	}

	tests := []struct {
		name string
		run  func() error
	}{
		{name: "missing file", run: func() error { _, err := FromFile(filepath.Join(dir, "missing.json")); return err }},
		{name: "malformed file", run: func() error { _, err := FromFile(bad); return err }},
		{name: "missing directory", run: func() error {
			return Write(filepath.Join(dir, "nope", "baseline.json"), "", "example.com/m", "est", nil)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.run(); err == nil {
				t.Error("succeeded, want an error")
			}
		})
	}
}

func TestFileTokenizer(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	old := filepath.Join(dir, "old.json")
	// A file written before the tokenizer field existed.
	body := `{"ref": "abc", "generated_at": "2026-01-01T00:00:00Z", "module_path": "example.com/m", "packages": {}}`
	if err := os.WriteFile(old, []byte(body), 0o600); err != nil {
		t.Fatalf("seeding old file: %v", err)
	}

	tests := []struct {
		name  string
		write bool   // write the file with tokenizer instead of reading old
		tok   string // tokenizer passed to Write
		want  string
	}{
		{name: "o200k round-trips", write: true, tok: "o200k", want: "o200k"},
		{name: "est round-trips", write: true, tok: "est", want: "est"},
		{name: "empty records est", write: true, tok: "", want: "est"},
		{name: "old file reads as est", want: "est"},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := old
			if tt.write {
				path = filepath.Join(dir, "b"+strconv.Itoa(i)+".json")
				if err := Write(path, "abc", "example.com/m", tt.tok, nil); err != nil {
					t.Fatalf("Write: %v", err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var raw struct {
					Tokenizer string `json:"tokenizer"`
				}
				if err := json.Unmarshal(data, &raw); err != nil {
					t.Fatal(err)
				}
				if raw.Tokenizer != tt.want {
					t.Errorf("file records tokenizer %q, want %q", raw.Tokenizer, tt.want)
				}
			}
			b, err := FromFile(path)
			if err != nil {
				t.Fatalf("FromFile: %v", err)
			}
			if got := b.Tokenizer(); got != tt.want {
				t.Errorf("Tokenizer() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFileFunctions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pkgs := map[string]metrics.RawMetrics{"example.com/m/a": {}, "example.com/m/b": {}}
	funcs := map[string][]metrics.FunctionInfo{
		"example.com/m/a": {
			{Name: "Parse", Fingerprint: 0xff, Cognitive: 4, File: "a.go", Line: 3},
			{Receiver: "T", Name: "Run", Fingerprint: 0xfedcba9876543210, Cognitive: 12},
		},
		"example.com/m/b": {},
	}
	path := filepath.Join(dir, "baseline.json")
	err := WriteContents(path, Contents{Ref: "abc", ModulePath: "example.com/m", Packages: pkgs, Functions: funcs})
	if err != nil {
		t.Fatalf("WriteContents: %v", err)
	}
	b, err := FromFile(path)
	if err != nil {
		t.Fatalf("FromFile: %v", err)
	}
	// File and line are not stored: a baseline function is only matched.
	want := []metrics.FunctionInfo{
		{Name: "Parse", Fingerprint: 0xff, Cognitive: 4},
		{Receiver: "T", Name: "Run", Fingerprint: 0xfedcba9876543210, Cognitive: 12},
	}
	if got, ok := b.Functions("example.com/m/a"); !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("Functions(a) = %+v, %v, want %+v", got, ok, want)
	}
	if got, ok := b.Functions("example.com/m/b"); !ok || len(got) != 0 {
		t.Errorf("Functions(b) = %+v, %v, want recorded and empty", got, ok)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Functions map[string][]map[string]any `json:"functions"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if fp := raw.Functions["example.com/m/a"][0]["fingerprint"]; fp != "00000000000000ff" {
		t.Errorf("fingerprint recorded as %v, want 16 hex digits", fp)
	}
	if _, ok := raw.Functions["example.com/m/a"][0]["file"]; ok {
		t.Error("baseline file records a function's file, want only receiver, name, fingerprint and cognitive")
	}

	t.Run("file without functions", func(t *testing.T) {
		t.Parallel()
		old := filepath.Join(t.TempDir(), "old.json")
		if err := Write(old, "abc", "example.com/m", "est", pkgs); err != nil {
			t.Fatal(err)
		}
		b, err := FromFile(old)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := b.Functions("example.com/m/a"); ok {
			t.Error("Functions of a file that records none reported found")
		}
	})
	t.Run("bad fingerprint", func(t *testing.T) {
		t.Parallel()
		bad := filepath.Join(t.TempDir(), "bad.json")
		body := `{"ref": "", "module_path": "example.com/m", "packages": {},
			"functions": {"example.com/m/a": [{"name": "F", "fingerprint": "xyz", "cognitive": 1}]}}`
		if err := os.WriteFile(bad, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := FromFile(bad); err == nil {
			t.Error("FromFile accepted a malformed fingerprint")
		}
	})
}
