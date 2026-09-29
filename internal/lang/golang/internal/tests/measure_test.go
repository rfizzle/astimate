package tests

import (
	"go/ast"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// TestMeasureUntestedRefinements counts, for the note in calibration/notes,
// the exported funcs and methods of every package of one module and how
// many of them untested_exports counts under each set of the SPEC.md 6.4
// refinements: none (the package's own tests only), references from other
// packages' tests, the closed interface list, and both. ASTIMATE_MEASURE_UNTESTED
// names the module directory, or "std" for the standard library. It logs
// one line per package whose count changes and the module totals. It loads
// a whole module, so it runs only when the variable is set and make check
// skips it.
func TestMeasureUntestedRefinements(t *testing.T) {
	target := os.Getenv("ASTIMATE_MEASURE_UNTESTED")
	if target == "" {
		t.Skip("set ASTIMATE_MEASURE_UNTESTED to std or a module directory")
	}
	var m *load.Module
	if target == "std" {
		var err error
		if m, _, err = load.Stdlib(t.Context(), packages.Load); err != nil {
			t.Fatal(err)
		}
	} else {
		m = loadRoot(t, target)
	}
	variants := []struct {
		name string
		on   rules
	}{
		{"own", 0},
		{"other", ruleOtherTests},
		{"iface", ruleStdInterfaces},
		{"both", allRules},
	}
	totals := make([]int, len(variants))
	exports, changed := 0, 0
	// The index does not depend on the refinements, so one serves all.
	var refs Refs
	start := time.Now()
	refs.build(m)
	t.Logf("index build: %v", time.Since(start).Round(time.Microsecond))
	var b strings.Builder
	for _, path := range m.Paths {
		p := m.Pkgs[path]
		exports += countedExports(m, p)
		counts := make([]int, len(variants))
		var left []string
		for i, v := range variants {
			c := untested(m, &refs, p, v.on)
			counts[i] = c.Untested
			totals[i] += counts[i]
			left = c.Names
		}
		if counts[0] == counts[len(counts)-1] {
			continue
		}
		changed++
		b.Reset()
		b.WriteString(path)
		for i, v := range variants {
			b.WriteString(" " + v.name + "=" + strconv.Itoa(counts[i]))
		}
		b.WriteString(" left=[" + strings.Join(left, " ") + "]")
		t.Log(b.String())
	}
	b.Reset()
	b.WriteString("packages=" + strconv.Itoa(len(m.Paths)) + " changed=" + strconv.Itoa(changed) + " exports=" + strconv.Itoa(exports))
	for i, v := range variants {
		b.WriteString(" " + v.name + "=" + strconv.Itoa(totals[i]))
	}
	t.Log(b.String())
}

// countedExports counts the exported funcs and methods untested_exports
// considers in p, directive-excluded ones left out.
func countedExports(m *load.Module, p *packages.Package) int {
	n := 0
	generated := m.GeneratedNames(p)
	for _, f := range p.Syntax {
		if isGeneratedTree(m, p, f, generated) {
			continue
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				if _, _, kind := exportKey(p.TypesInfo, fd); kind == exportCounted {
					n++
				}
			}
		}
	}
	return n
}
