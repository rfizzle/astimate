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

// couplingModule writes a module of six packages. iface exports three
// interface types, one an interface literal, one a definition naming
// io.Reader and one an alias of io.Writer, and an unexported struct. dotted
// exports only a function. side blank-imports iface and dot-imports dotted.
// mixed imports iface and dotted and exports one interface and two structs.
// user imports iface and mixed and exports one interface and four structs.
// lonely has no
// exported types and no internal edges.
func couplingModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/coupling\n\ngo 1.27\n")
	for dir, src := range map[string]string{
		"iface": "package iface\n\nimport \"io\"\n\n// Store gets.\ntype Store interface{ Get() int }\n\n" +
			"// Reader reads.\ntype Reader io.Reader\n\n// Writer writes.\ntype Writer = io.Writer\n\ntype impl struct{}\n",
		"dotted": "package dotted\n\n// F answers.\nfunc F() int { return 1 }\n",
		"side": "package side\n\nimport (\n\t_ \"example.com/coupling/iface\"\n\t. \"example.com/coupling/dotted\"\n)\n\n" +
			"// G answers.\nfunc G() int { return F() }\n",
		"mixed": "package mixed\n\nimport (\n\t\"example.com/coupling/dotted\"\n\t\"example.com/coupling/iface\"\n)\n\n" +
			"// I stores.\ntype I interface{ iface.Store }\n\n// S is concrete.\ntype S struct{}\n\n" +
			"// U is concrete.\ntype U struct{ N int }\n\n// H answers.\nfunc H() int { return dotted.F() }\n",
		"user": "package user\n\nimport (\n\t\"example.com/coupling/iface\"\n\t\"example.com/coupling/mixed\"\n)\n\n" +
			"// T holds a store.\ntype T struct {\n\tS iface.Store\n\tM mixed.S\n}\n\n" +
			"// I is abstract.\ntype I interface{ M() }\n\n" +
			"type (\n\t// A is concrete.\n\tA struct{}\n\t// B is concrete.\n\tB struct{}\n\t// C is concrete.\n\tC struct{}\n)\n",
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
		var fanIn, fanOut int
		for pkg, want := range map[string]couplingWant{
			// fan-in 3 (user, mixed, and side's blank import), fan-out 0:
			// stable; all three exported types are interfaces, the
			// definition and the alias naming one included.
			"example.com/coupling/iface": {"0", "1", "0"},
			// fan-in 2 (mixed, and side's dot import), fan-out 0; no types.
			"example.com/coupling/dotted": {"0", "null", "null"},
			// fan-in 0, fan-out 2 through a blank and a dot import.
			"example.com/coupling/side": {"1", "null", "null"},
			// fan-in 1, fan-out 2, one interface of three types: 2/3 and
			// 1/3 are rounded, and the distance is 0.
			"example.com/coupling/mixed": {"0.667", "0.333", "0"},
			// fan-in 0, fan-out 2: unstable; one interface of five types.
			// Unrounded, |0.2 + 1 - 1| is 0.19999999999999996.
			"example.com/coupling/user": {"1", "0.2", "0.2"},
			// no internal edges and no exported types: all undefined.
			"example.com/coupling/lonely": {"null", "null", "null"},
		} {
			t.Run(pkg, func(t *testing.T) {
				m, err := e.Extract(t.Context(), mod, pkg)
				if err != nil {
					t.Fatalf("Extract: %v", err)
				}
				checkCoupling(t, &m, want)
				fanIn += m.FanIn
				fanOut += m.InternalImports
			})
		}
		// Every edge, the blank and dot imports included, counts once on
		// each side.
		if fanIn != 6 || fanOut != 6 {
			t.Errorf("sum(fan_in) = %d, sum(internal_imports) = %d, want 6 and 6", fanIn, fanOut)
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
