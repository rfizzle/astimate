package golang

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// optText renders an optional metric exactly, "null" when nil.
func optText(p *float64) string {
	if p == nil {
		return "null"
	}
	return strconv.FormatFloat(*p, 'g', -1, 64)
}

// couplingWant is the expected instability, abstractness and
// main_sequence_distance of one package, as optText renders them.
type couplingWant struct {
	instability, abstractness, distance string
}

func checkCoupling(t *testing.T, m *metrics.RawMetrics, want couplingWant) {
	t.Helper()
	got := couplingWant{optText(m.Instability), optText(m.Abstractness), optText(m.MainSequenceDistance)}
	if got != want {
		t.Errorf("instability, abstractness, main_sequence_distance = %+v, want %+v", got, want)
	}
}

// couplingModule writes a module in which iface's only exported type is an
// interface imported by user, whose only exported type is a struct, and
// lonely has no exported types and no internal edges.
func couplingModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/coupling\n\ngo 1.27\n")
	for dir, src := range map[string]string{
		"iface":  "package iface\n\n// Store gets.\ntype Store interface{ Get() int }\n\ntype impl struct{}\n",
		"user":   "package user\n\nimport \"example.com/coupling/iface\"\n\n// T holds a store.\ntype T struct{ S iface.Store }\n",
		"lonely": "package lonely\n\n// F answers.\nfunc F() int { return 1 }\n",
	} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(root, dir, dir+".go"), src)
	}
	return root
}

func TestCouplingMetrics(t *testing.T) {
	t.Run("synthetic module", func(t *testing.T) {
		root := couplingModule(t)
		e := New()
		mod := &metrics.ModuleContext{Root: root}
		for pkg, want := range map[string]couplingWant{
			// fan-in 1, fan-out 0: stable; only exported type an interface.
			"example.com/coupling/iface": {"0", "1", "0"},
			// fan-in 0, fan-out 1: unstable; only exported type concrete.
			"example.com/coupling/user": {"1", "0", "0"},
			// no internal edges and no exported types: all undefined.
			"example.com/coupling/lonely": {"null", "null", "null"},
		} {
			t.Run(pkg, func(t *testing.T) {
				m, err := e.Extract(t.Context(), mod, pkg)
				if err != nil {
					t.Fatalf("Extract: %v", err)
				}
				checkCoupling(t, &m, want)
			})
		}
	})
	// The fixture declares no exported types, so abstractness and distance
	// are null everywhere, which the goldens cannot express since they omit
	// null v1 fields.
	t.Run("fixture", func(t *testing.T) {
		e := New()
		mod := &metrics.ModuleContext{Root: fixtureRoot(t)}
		for pkg, want := range map[string]couplingWant{
			"example.com/fixture/hub":     {"0", "null", "null"},
			"example.com/fixture/a":       {"1", "null", "null"},
			"example.com/fixture/b":       {"1", "null", "null"},
			"example.com/fixture/hidden":  {"1", "null", "null"},
			"example.com/fixture/tested":  {"1", "null", "null"},
			"example.com/fixture/dupes":   {"null", "null", "null"},
			"example.com/fixture/trivial": {"null", "null", "null"},
		} {
			t.Run(pkg, func(t *testing.T) {
				m, err := e.Extract(t.Context(), mod, pkg)
				if err != nil {
					t.Fatalf("Extract: %v", err)
				}
				checkCoupling(t, &m, want)
			})
		}
	})
}
