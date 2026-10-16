package report

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// skippedCheck returns a passing check of one package and the module row
// in a module with two packages that do not load: remote, which the check
// selected, its first error spread over two lines as the go command
// writes some, and tools, which it did not; and with gen, none of whose
// files is built, which only the JSON lists.
func skippedCheck(t *testing.T) *Check {
	t.Helper()
	row := func(path string) CheckedPackage {
		res := gate.Evaluate(metrics.RawMetrics{}, &metrics.RawMetrics{}, nil, nil)
		r := Build(&Input{Language: "go", PackagePath: path, ModulePath: "example.com/app",
			Params: params(), ConfigVersion: uncalibratedVersion})
		ApplyGate(&r, "a1b2c3d", &metrics.RawMetrics{}, &res)
		passes := 0.0
		return CheckedPackage{Report: r, BaseAgentPasses: &passes}
	}
	module := row(metrics.ModuleRowID)
	return &Check{
		Module:   &module,
		Packages: []CheckedPackage{row("app")},
		Skipped: []Skipped{
			{Package: "gen", Reason: "build constraints exclude all Go files"},
			{Package: "remote", Reason: "missing module", Selected: true,
				Error: "loading example.com/app/remote: remote.go:4:2: no required module provides package example.com/absent/client;\n\tto add it: go get example.com/absent/client"},
			{Package: "tools", Reason: "type error in test files",
				Error: "loading example.com/app/tools: tools_test.go:3:9: undefined: missing"},
		},
	}
}

// TestSkippedFormats renders the skipped packages in every check format
// and compares each with its golden file: one line, warning line or
// annotation per package after the findings, and a skipped list on the
// JSON module row alone.
func TestSkippedFormats(t *testing.T) {
	t.Parallel()

	for _, f := range checkFormats() {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()

			got, err := f.write(skippedCheck(t))
			if err != nil {
				t.Fatal(err)
			}
			compareGolden(t, filepath.Join("testdata", "skipped", f.name+".golden"), got)
		})
	}
}

// TestSkippedJSONOnModuleRow checks that only the module row carries
// skipped, that a check without skipped packages has no such key, and
// that the text says nothing changed only when no skipped package was
// selected either.
func TestSkippedJSONOnModuleRow(t *testing.T) {
	t.Parallel()

	c := skippedCheck(t)
	var b strings.Builder
	if err := WriteCheckJSON(&b, c); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(b.String()), &rows); err != nil {
		t.Fatal(err)
	}
	if _, ok := rows[0]["skipped"]; !ok {
		t.Error("module row has no skipped list")
	}
	if _, ok := rows[1]["skipped"]; ok {
		t.Error("package row carries the skipped list")
	}

	c.Skipped = nil
	b.Reset()
	if err := WriteCheckJSON(&b, c); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), `"skipped"`) {
		t.Errorf("JSON without skipped packages mentions skipped:\n%s", b.String())
	}

	for _, tc := range []struct {
		name     string
		selected bool
		want     bool
	}{
		{"unselected", false, true},
		{"selected", true, false},
	} {
		c := &Check{Skipped: []Skipped{{Package: "remote", Reason: "type error", Error: "e", Selected: tc.selected}}}
		var text strings.Builder
		if err := WriteCheckText(&text, c); err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(text.String(), "no changed packages"); got != tc.want {
			t.Errorf("%s: says no changed packages = %v, want %v:\n%s", tc.name, got, tc.want, text.String())
		}
	}
}
