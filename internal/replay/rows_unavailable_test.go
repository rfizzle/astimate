package replay

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/report"
)

// TestPackageRowBaselineUnavailable checks that a row whose package failed
// to load at the parent records baseline "unavailable" beside new, and that
// any other row leaves the field out.
func TestPackageRowBaselineUnavailable(t *testing.T) {
	t.Parallel()

	row := newPackageRow("c", &report.CheckedPackage{Report: report.Report{
		PackagePath: "internal/broker", BaselineUnavailable: "type error: broker.go:3:18: bad",
	}})
	if !row.New || row.Base != nil || row.Baseline != baselineUnavailable {
		t.Errorf("row new %v, base %v, baseline %q, want new, no base and %q", row.New, row.Base, row.Baseline, baselineUnavailable)
	}
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"new":true,"baseline":"unavailable"`) {
		t.Errorf("row JSON lacks baseline unavailable beside new: %s", data)
	}

	plain := newPackageRow("c", &report.CheckedPackage{Report: report.Report{PackagePath: "a"}})
	if data, err = json.Marshal(plain); err != nil {
		t.Fatal(err)
	}
	if plain.Baseline != "" || strings.Contains(string(data), `"baseline"`) {
		t.Errorf("new row JSON has a baseline field: %s", data)
	}
}
