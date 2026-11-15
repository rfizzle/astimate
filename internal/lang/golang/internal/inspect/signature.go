package inspect

import "go/ast"

// Signature hashes (SPEC.md 8.1, Reshaped exports).
//
// An exported function's signature hash covers its type parameters, its
// parameters and its results, in that order, each list closed by
// sigEnd: per entry, the shape of its type expression as the body
// fingerprint codes it (fpCode), with every identifier by its name and
// every literal, such as an array length, by its text. A field declaring
// several names is one entry per name, so func(a, b int) and
// func(int, int) hash alike, and the names of parameters and results,
// those of a func type's parameters inside a type included, are not
// hashed. The type parameters of the function and of its receiver hash by
// position, so renaming T to U changes nothing, while their number and
// constraints count. The receiver itself is not in the hash: methods are
// matched by receiver base type and name, and a change between T and *T
// is not seen. The hash is syntactic: renaming an import or naming a type
// through an alias changes it, and a type that changed in another
// declaration does not.

const (
	// sigEnd closes a list of entries or a name. It and sigParam sit
	// above every kind byte fpCode returns.
	sigEnd uint32 = 0xfe
	// sigParam marks a reference to a type parameter, followed by its
	// position.
	sigParam uint32 = 0xfd
)

// Signature returns the signature hash of fn, never 0. It walks only
// fn's type and receiver, never its body.
func Signature(fn *ast.FuncDecl) uint64 {
	var v sigHasher
	return v.sum(fn)
}

// sum returns the signature hash of fn, reusing v, so hashing every
// function of a package allocates one hasher.
func (v *sigHasher) sum(fn *ast.FuncDecl) uint64 {
	v.h, v.params = fpOffset, typeParams(fn)
	v.fields(fn.Type.TypeParams)
	v.fields(fn.Type.Params)
	v.fields(fn.Type.Results)
	if v.h == 0 {
		return 1
	}
	return v.h
}

// typeParams returns the position of each type parameter name of fn and
// of its receiver's type, or nil when it has none.
func typeParams(fn *ast.FuncDecl) map[string]uint32 {
	var names []*ast.Ident
	if fn.Type.TypeParams != nil {
		for _, f := range fn.Type.TypeParams.List {
			names = append(names, f.Names...)
		}
	}
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		t := fn.Recv.List[0].Type
		if s, ok := t.(*ast.StarExpr); ok {
			t = s.X
		}
		switch x := t.(type) {
		case *ast.IndexExpr:
			names = appendIdent(names, x.Index)
		case *ast.IndexListExpr:
			for _, e := range x.Indices {
				names = appendIdent(names, e)
			}
		}
	}
	if len(names) == 0 {
		return nil
	}
	out := make(map[string]uint32, len(names))
	for i, n := range names {
		out[n.Name] = uint32(i)
	}
	return out
}

// appendIdent appends e to names when it is an identifier.
func appendIdent(names []*ast.Ident, e ast.Expr) []*ast.Ident {
	if id, ok := e.(*ast.Ident); ok {
		return append(names, id)
	}
	return names
}

// sigHasher is the ast.Visitor that hashes a signature's type
// expressions: h is the hash so far and params the position of each type
// parameter name.
type sigHasher struct {
	h      uint64
	params map[string]uint32
}

// fields mixes each entry of fl, one per declared name or one for an
// unnamed field, as a field code followed by its type, then sigEnd.
func (v *sigHasher) fields(fl *ast.FieldList) {
	if fl != nil {
		for _, f := range fl.List {
			for range max(1, len(f.Names)) {
				v.h = fpMix(v.h, fpField)
				ast.Walk(v, f.Type)
			}
		}
	}
	v.h = fpMix(v.h, sigEnd)
}

// Visit implements ast.Visitor: each node's code on entry and a close code
// on exit, identifiers by name, a type parameter by its position, literals
// by text, and a func type's parameters and results as fields does,
// without their names.
func (v *sigHasher) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		v.h = fpMix(v.h, fpClose)
		return nil
	}
	v.h = fpMix(v.h, fpCode(n))
	switch x := n.(type) {
	case *ast.Ident:
		if i, ok := v.params[x.Name]; ok {
			v.h = fpMix(fpMix(v.h, sigParam), i)
		} else {
			v.h = mixText(v.h, x.Name)
		}
	case *ast.BasicLit:
		v.h = mixText(v.h, x.Value)
	case *ast.FuncType:
		v.fields(x.Params)
		v.fields(x.Results)
		v.h = fpMix(v.h, fpClose)
		return nil
	}
	return v
}

// mixText mixes each byte of s into h, then sigEnd, so adjacent names
// cannot run together.
func mixText(h uint64, s string) uint64 {
	for i := range len(s) {
		h = fpMix(h, uint32(s[i]))
	}
	return fpMix(h, sigEnd)
}
