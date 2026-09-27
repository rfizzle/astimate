package metrics

import "context"

// FunctionInfo describes one function or method of a package for the
// function-level baseline diff behind changed_func_cognitive_max (SPEC.md
// 6.5). Functions are matched by package, Receiver and Name; a matched
// function is modified when its Fingerprint differs.
type FunctionInfo struct {
	// Receiver is the method's receiver base type name, without a pointer
	// or type parameters; empty for a plain function.
	Receiver string
	// Name is the function or method name.
	Name string
	// Fingerprint hashes the function body's normalized token stream, so
	// edits to comments, formatting, identifier names or literal values do
	// not change it.
	Fingerprint uint64
	// Cognitive is the function's cognitive complexity.
	Cognitive int
	// File is the declaring file relative to the package directory, in
	// slash form; empty when unknown, as in a baseline file.
	File string
	// Line is the line of the declaration in File; 0 when unknown.
	Line int
}

// QualifiedName returns Name, prefixed with Receiver and a dot for a
// method.
func (f *FunctionInfo) QualifiedName() string {
	if f.Receiver == "" {
		return f.Name
	}
	return f.Receiver + "." + f.Name
}

// FunctionLister is an optional interface an Extractor implements when it
// can list the functions of a package with fingerprints. Without it,
// changed_func_cognitive_max stays null and its rule is skipped. Callers
// type-assert an Extractor to it.
type FunctionLister interface {
	// Functions returns the functions and methods of pkg's non-test files,
	// in declaration order. Like Detailer.Details, the result reflects the
	// most recent Extract of pkg on the same mod, and an implementation may
	// call Extract itself when nothing is recorded yet.
	Functions(ctx context.Context, mod *ModuleContext, pkg string) ([]FunctionInfo, error)
}

// ChangedFunctions returns the functions of head, in head order, that were
// added or modified relative to base, both lists being one package's
// functions. A head function is unchanged when base holds a function with
// the same Receiver, Name and Fingerprint that no earlier head function
// matched; names that may repeat, such as init, are matched as a multiset.
// A renamed function, or a method moved to another receiver, is therefore
// new. Functions only in base were deleted and are not reported.
func ChangedFunctions(base, head []FunctionInfo) []FunctionInfo {
	type key struct {
		receiver, name string
		fingerprint    uint64
	}
	left := make(map[key]int, len(base))
	for i := range base {
		left[key{base[i].Receiver, base[i].Name, base[i].Fingerprint}]++
	}
	var changed []FunctionInfo
	for i := range head {
		k := key{head[i].Receiver, head[i].Name, head[i].Fingerprint}
		if left[k] > 0 {
			left[k]--
			continue
		}
		changed = append(changed, head[i])
	}
	return changed
}

// MostComplex returns the index in fns of the function with the highest
// Cognitive, the first on ties, and -1 when fns is empty.
func MostComplex(fns []FunctionInfo) int {
	best := -1
	for i := range fns {
		if best < 0 || fns[i].Cognitive > fns[best].Cognitive {
			best = i
		}
	}
	return best
}
