// Package stdiface knows the closed list of interfaces of SPEC.md 6.4 that
// the runtime or the standard library calls a method through, and whether
// a method implements one of them.
package stdiface

import (
	"go/token"
	"go/types"
	"slices"
)

// stdMethod is one method of a plain interface of the closed list: its
// name, parameter types and result types.
type stdMethod struct {
	name            string
	params, results []types.Type
}

// plainStdInterfaces returns, as method lists, the interfaces of the closed
// list of SPEC.md 6.4 whose methods mention only predeclared types, and the
// method-set conventions the errors package calls without an interface.
// Predeclared types are shared by every type-check, so these interfaces
// can be checked against any package's types with types.Implements.
func plainStdInterfaces() [][]stdMethod {
	var (
		str    = types.Typ[types.String]
		num    = types.Typ[types.Int]
		boolT  = types.Typ[types.Bool]
		bytes  = types.NewSlice(types.Typ[types.Byte])
		errT   = types.Universe.Lookup("error").Type()
		anyT   = types.Universe.Lookup("any").Type()
		errs   = types.NewSlice(errT)
		strs   = types.NewSlice(str)
		none   = []types.Type(nil)
		one    = func(t ...types.Type) []types.Type { return t }
		sortIf = []stdMethod{
			{"Len", none, one(num)},
			{"Less", one(num, num), one(boolT)},
			{"Swap", one(num, num), none},
		}
		flagIf = []stdMethod{{"String", none, one(str)}, {"Set", one(str), one(errT)}}
		heapIf = append(slices.Clip(sortIf),
			stdMethod{"Push", one(anyT), none},
			stdMethod{"Pop", none, one(anyT)})
	)
	list := [][]stdMethod{
		{{"String", none, one(str)}},            // fmt.Stringer
		{{"GoString", none, one(str)}},          // fmt.GoStringer
		sortIf,                                  // sort.Interface
		heapIf,                                  // heap.Interface
		{{"Read", one(bytes), one(num, errT)}},  // io.Reader
		{{"Write", one(bytes), one(num, errT)}}, // io.Writer
		{{"Close", none, one(errT)}},            // io.Closer
		{{"Scan", one(anyT), one(errT)}},        // sql.Scanner
		flagIf,                                  // flag.Value
		{{"Flush", none, none}},                 // http.Flusher
		append(slices.Clip(flagIf), stdMethod{"Type", none, one(str)}),                                      // pflag.Value
		{{"Append", one(str), one(errT)}, {"Replace", one(strs), one(errT)}, {"GetSlice", none, one(strs)}}, // pflag.SliceValue
		{{"Unwrap", none, one(errT)}},   // errors: Unwrap() error
		{{"Unwrap", none, one(errs)}},   // errors: Unwrap() []error
		{{"Is", one(errT), one(boolT)}}, // errors: Is(error) bool
		{{"As", one(anyT), one(boolT)}}, // errors: As(any) bool
	}
	// encoding.TextMarshaler and TextUnmarshaler, encoding.BinaryMarshaler
	// and BinaryUnmarshaler, json.Marshaler and Unmarshaler.
	for _, format := range [...]string{"Text", "Binary", "JSON"} {
		list = append(list,
			[]stdMethod{{"Marshal" + format, none, one(bytes, errT)}},
			[]stdMethod{{"Unmarshal" + format, one(bytes), one(errT)}})
	}
	return list
}

// namedStdInterface is an interface of the closed list whose methods
// mention a named type of its own package, such as fmt.State. It is looked
// up in that package as the method's own signature reaches it, since a
// type from another type-check is never identical.
type namedStdInterface struct {
	pkg, name, method string
}

// namedStdInterfaces returns the interfaces of the closed list that
// plainStdInterfaces cannot build.
func namedStdInterfaces() []namedStdInterface {
	return []namedStdInterface{
		{"fmt", "Formatter", "Format"},
		{"io", "ReaderFrom", "ReadFrom"},
		{"io", "WriterTo", "WriteTo"},
		{"net/http", "Handler", "ServeHTTP"},
		{"net/http", "Pusher", "Push"},
		{"database/sql/driver", "Valuer", "Value"},
		{"log/slog", "LogValuer", "LogValue"},
	}
}

// Set indexes the closed list of SPEC.md 6.4 by method name.
type Set struct {
	// plain maps a method name to the predeclared-only interfaces
	// declaring it, error among them.
	plain map[string][]*types.Interface
	// named maps a method name to the interfaces to look up in the
	// package its signature names.
	named map[string][]namedStdInterface
}

// New builds the plain interfaces of the closed list and
// indexes the whole list by method name.
func New() Set {
	s := Set{
		plain: make(map[string][]*types.Interface),
		named: make(map[string][]namedStdInterface),
	}
	add := func(iface *types.Interface) {
		for m := range iface.Methods() {
			s.plain[m.Name()] = append(s.plain[m.Name()], iface)
		}
	}
	add(types.Universe.Lookup("error").Type().Underlying().(*types.Interface))
	tuple := func(ts []types.Type) *types.Tuple {
		vars := make([]*types.Var, len(ts))
		for i, t := range ts {
			vars[i] = types.NewParam(token.NoPos, nil, "", t)
		}
		return types.NewTuple(vars...)
	}
	for _, methods := range plainStdInterfaces() {
		fns := make([]*types.Func, len(methods))
		for i, m := range methods {
			sig := types.NewSignatureType(nil, nil, nil, tuple(m.params), tuple(m.results), false)
			fns[i] = types.NewFunc(token.NoPos, nil, m.name, sig)
		}
		add(types.NewInterfaceType(fns, nil).Complete())
	}
	for _, n := range namedStdInterfaces() {
		s.named[n.method] = append(s.named[n.method], n)
	}
	return s
}

// Covers reports whether fn, a method, is a method of an interface of the
// closed list that its receiver type, or a pointer to it, implements. The
// receiver is taken as the method declares it, so a generic type is
// checked with its own type parameters.
func (s Set) Covers(fn *types.Func) bool {
	if fn == nil {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	recv := NamedOf(sig.Recv().Type())
	if recv == nil {
		return false
	}
	for _, iface := range s.plain[fn.Name()] {
		if Implements(recv, iface) {
			return true
		}
	}
	if fn.Name() == "Hijack" && isHijack(sig) {
		return true
	}
	for _, n := range s.named[fn.Name()] {
		pkg := signaturePackage(sig, n.pkg)
		if pkg == nil {
			continue
		}
		obj, ok := pkg.Scope().Lookup(n.name).(*types.TypeName)
		if !ok {
			continue
		}
		if iface, ok := obj.Type().Underlying().(*types.Interface); ok && Implements(recv, iface) {
			return true
		}
	}
	return false
}

// isHijack reports whether sig is that of http.Hijacker's Hijack:
// func() (net.Conn, *bufio.ReadWriter, error). Its types come from net and
// bufio, not net/http, so it is matched by the types' own names rather
// than looked up in the package that declares the interface.
func isHijack(sig *types.Signature) bool {
	res := sig.Results()
	if sig.Params().Len() != 0 || res.Len() != 3 || sig.Variadic() {
		return false
	}
	is := func(t types.Type, path, name string) bool {
		n := NamedOf(t)
		return n != nil && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == path && n.Obj().Name() == name
	}
	_, ptr := types.Unalias(res.At(1).Type()).(*types.Pointer)
	return is(res.At(0).Type(), "net", "Conn") && ptr && is(res.At(1).Type(), "bufio", "ReadWriter") &&
		types.Identical(res.At(2).Type(), types.Universe.Lookup("error").Type())
}

// signaturePackage returns the package at path that a named type among
// sig's parameter and result types belongs to, looking through pointers,
// or nil when none does. A method implementing an interface of that
// package mentions one of its types, so this finds the package as the
// method's own type-check sees it.
func signaturePackage(sig *types.Signature, path string) *types.Package {
	for _, tup := range [...]*types.Tuple{sig.Params(), sig.Results()} {
		for v := range tup.Variables() {
			if n := NamedOf(v.Type()); n != nil && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == path {
				return n.Obj().Pkg()
			}
		}
	}
	return nil
}

// NamedOf returns the named type of t after dereferencing a pointer, or nil.
func NamedOf(t types.Type) *types.Named {
	t = types.Unalias(t)
	if ptr, ok := t.(*types.Pointer); ok {
		t = types.Unalias(ptr.Elem())
	}
	n, _ := t.(*types.Named)
	return n
}

// Implements reports whether t or *t implements iface.
func Implements(t types.Type, iface *types.Interface) bool {
	return types.Implements(t, iface) || types.Implements(types.NewPointer(t), iface)
}
