// Package surface tells what a change did to a package's exported surface
// that exported_symbols does not count (SPEC.md 8.1, Reshaped exports):
// the exports whose signature changed and those made unexported since the
// baseline. Package judge gives what it finds to the gate, which reports
// it as a heads-up near the exported_symbols ceiling.
package surface

import (
	"cmp"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics/inventory"
)

// Reshaped compares d, a package's details at head, with what its baseline
// recorded: baseFns, its functions, and baseExports, its exported
// declarations, each nil when the baseline recorded none. It returns the
// names a heads-up lists, the exports whose signature changed and then
// those made unexported, each part sorted by qualified name, and where the
// first is declared at head, relative to the package directory.
//
// An export whose signature changed is an exported function or method at
// head whose receiver and name match baseline functions that carry a
// signature hash (inventory.FunctionInfo.Signature), none of them its own.
// An export made unexported is one of baseExports that head does not
// export under its receiver and name, while head declares an unexported
// symbol of the same kind and receiver whose name equals it ignoring case,
// as Options becomes options and HTTPClient httpClient. A symbol deleted
// or moved to another package is neither.
func Reshaped(d *inventory.Details, baseFns []inventory.FunctionInfo, baseExports []inventory.Symbol) (names []string, at inventory.Position) {
	add := func(name string, pos inventory.Position) {
		if len(names) == 0 {
			at = pos
		}
		names = append(names, name)
	}
	for _, f := range signatureChanges(d.Functions, baseFns) {
		add(f.QualifiedName()+" (signature changed)", inventory.Position{File: f.File, Line: f.Line})
	}
	for _, u := range unexported(d, baseExports) {
		add(qualified(&u[0])+" (unexported as "+u[1].Name+")", u[1].Position)
	}
	return names, at
}

// key identifies a declaration: its kind, which a function's key leaves
// empty, its receiver and its name.
type key struct{ kind, recv, name string }

// signatureChanges returns the functions of head whose signature differs
// from that of every function of base with its receiver and name, among
// those carrying one, sorted by qualified name.
func signatureChanges(head, base []inventory.FunctionInfo) []inventory.FunctionInfo {
	before := make(map[key][]uint64, len(base))
	for _, f := range base {
		if f.Signature != 0 {
			k := key{recv: f.Receiver, name: f.Name}
			before[k] = append(before[k], f.Signature)
		}
	}
	var changed []inventory.FunctionInfo
	for _, f := range head {
		sigs := before[key{recv: f.Receiver, name: f.Name}]
		if f.Signature != 0 && len(sigs) > 0 && !slices.Contains(sigs, f.Signature) {
			changed = append(changed, f)
		}
	}
	slices.SortFunc(changed, func(a, b inventory.FunctionInfo) int { return cmp.Compare(a.QualifiedName(), b.QualifiedName()) })
	return changed
}

// unexported returns each export of base that d made unexported, paired
// with the head declaration that took its place, sorted by the export's
// qualified name.
func unexported(d *inventory.Details, base []inventory.Symbol) [][2]inventory.Symbol {
	if len(base) == 0 {
		return nil
	}
	exported := make(map[key]bool, len(d.Exports))
	for _, s := range d.Exports {
		exported[key{recv: s.Receiver, name: s.Name}] = true
	}
	lowered := make(map[key]inventory.Symbol, len(d.Unexported))
	for _, s := range d.Unexported {
		k := key{s.Kind, s.Receiver, strings.ToLower(s.Name)}
		if _, ok := lowered[k]; !ok {
			lowered[k] = s
		}
	}
	var out [][2]inventory.Symbol
	for _, s := range base {
		if exported[key{recv: s.Receiver, name: s.Name}] {
			continue
		}
		if to, ok := lowered[key{s.Kind, s.Receiver, strings.ToLower(s.Name)}]; ok {
			out = append(out, [2]inventory.Symbol{s, to})
		}
	}
	slices.SortFunc(out, func(a, b [2]inventory.Symbol) int { return cmp.Compare(qualified(&a[0]), qualified(&b[0])) })
	return out
}

// qualified returns s's name, prefixed with its receiver and a dot for a
// method.
func qualified(s *inventory.Symbol) string {
	if s.Receiver == "" {
		return s.Name
	}
	return s.Receiver + "." + s.Name
}
