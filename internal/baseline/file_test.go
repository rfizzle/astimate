package baseline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/metrics"
)

func TestFileRoundTrip(t *testing.T) {
	t.Parallel()

	ratio := 0.25
	pkgs := map[string]metrics.RawMetrics{
		"example.com/m/a": {Files: 2, SLOC: 40, Globals: 1, DuplicationPct: 12.5, HasTests: true},
		"example.com/m/b": {Files: 1, SLOC: 7, ConcreteParamRatio: &ratio},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")

	before := time.Now().UTC().Truncate(time.Second)
	if err := Write(path, "abc123", "example.com/m", pkgs); err != nil {
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
	if err := Write(path, "old", "example.com/m", map[string]metrics.RawMetrics{"example.com/m/a": {Files: 1}}); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if err := Write(path, "new", "example.com/m", nil); err != nil {
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
			return Write(filepath.Join(dir, "nope", "baseline.json"), "", "example.com/m", nil)
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
