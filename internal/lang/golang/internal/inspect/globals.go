package inspect

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// GlobalCounts is the hidden-contract state of one package: package-level
// variables that hold mutable state and init functions, which no exported
// signature reveals.
type GlobalCounts struct {
	// Globals is the number of names declared by package-level var specs
	// that hold mutable state, excluding the blank identifier (see Globals
	// for the specs left out).
	Globals int
	// InitFuncs is the number of top-level func init() declarations.
	InitFuncs int
	// Exported and Unexported are the counted global names, split by
	// visibility, in declaration order. They are a debug breakdown of
	// Globals.
	Exported, Unexported []string
	// Pos holds the declaring identifier of each counted global, in
	// declaration order, for annotations.
	Pos []token.Pos
	// Names holds the name of each counted global, index for index with
	// Pos.
	Names []string
}

// Globals counts the package-level variables that hold mutable state, and
// the init functions, declared in p's authored non-test files; a generated
// file's state is its generator's (see load.Module.AuthoredSyntax). It
// reads only the top-level declarations of each file, so variables declared
// inside function bodies never count. Constants are not state and are
// ignored. A var spec counts each of its names, so var a, b = 1, 2 is 2.
//
// A spec none of whose names is in written (ComplexityCounts.Written) is
// left out when it is one of three immutable idioms (SPEC.md 6.5):
//   - a sentinel error: every name has type error and every initializer is
//     a call to errors.New or fmt.Errorf whose arguments are all constant;
//   - an embedded file: the spec carries a //go:embed directive;
//   - build information: every name has a boolean, numeric or string basic
//     type and every initializer, if any, is constant, as for a value set
//     by -ldflags -X.
//
// The first and third need p's type information; without it, as for a
// package that carries only syntax, those specs count.
func Globals(m *load.Module, p *packages.Package, written map[string]bool) GlobalCounts {
	var c GlobalCounts
	k := varKinds{info: p.TypesInfo, written: written, pkgPath: p.PkgPath}
	if p.Types != nil {
		k.scope = p.Types.Scope()
	}
	for _, f := range m.AuthoredSyntax(p) {
		k.file = f
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				if d.Tok == token.VAR {
					c.addVars(d, &k)
				}
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.Name == "init" {
					c.InitFuncs++
				}
			}
		}
	}
	return c
}

// addVars counts every non-blank name declared by the specs of a var
// declaration that k does not find immutable.
func (c *GlobalCounts) addVars(d *ast.GenDecl, k *varKinds) {
	for _, s := range d.Specs {
		vs, ok := s.(*ast.ValueSpec)
		if !ok || k.immutable(d, vs) {
			continue
		}
		for _, n := range vs.Names {
			if n.Name == "_" {
				continue
			}
			c.Globals++
			c.Pos = append(c.Pos, n.Pos())
			c.Names = append(c.Names, n.Name)
			if n.IsExported() {
				c.Exported = append(c.Exported, n.Name)
			} else {
				c.Unexported = append(c.Unexported, n.Name)
			}
		}
	}
}

// varKinds decides which package-level var specs of one package hold no
// mutable state.
type varKinds struct {
	// info resolves the syntax of p.Syntax; a tree it does not cover, such
	// as a cgo package's source file, falls back to syntax and scope.
	info *types.Info
	// scope is the package scope, nil without type information.
	scope *types.Scope
	// written holds the names of the package-level variables that code
	// in the package assigns, increments or takes the address of.
	written map[string]bool
	// pkgPath is the package's import path.
	pkgPath string
	// file is the file whose declarations are being read, for resolving
	// its imports by name.
	file *ast.File
}

// immutable reports whether vs, a spec of d, is a sentinel error, an
// embedded file or build information that nothing in the package writes.
func (k *varKinds) immutable(d *ast.GenDecl, vs *ast.ValueSpec) bool {
	for _, n := range vs.Names {
		if k.written[n.Name] {
			return false
		}
	}
	return embedded(d, vs) || k.sentinel(vs) || k.buildInfo(vs)
}

// embedded reports whether vs carries a //go:embed directive: in its own
// doc comment, or in d's when d declares vs alone without parentheses.
func embedded(d *ast.GenDecl, vs *ast.ValueSpec) bool {
	if hasEmbed(vs.Doc) {
		return true
	}
	return !d.Lparen.IsValid() && hasEmbed(d.Doc)
}

// hasEmbed reports whether g holds a //go:embed directive line.
func hasEmbed(g *ast.CommentGroup) bool {
	return g != nil && slices.ContainsFunc(g.List, func(c *ast.Comment) bool {
		rest, ok := strings.CutPrefix(c.Text, "//go:embed")
		return ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t')
	})
}

// sentinel reports whether every name of vs has type error and every
// initializer is a call to errors.New or fmt.Errorf with constant
// arguments only.
func (k *varKinds) sentinel(vs *ast.ValueSpec) bool {
	if k.scope == nil || len(vs.Values) != len(vs.Names) {
		return false
	}
	errType := types.Universe.Lookup("error").Type()
	for i, n := range vs.Names {
		if n.Name != "_" {
			v, ok := k.scope.Lookup(n.Name).(*types.Var)
			if !ok || !types.Identical(v.Type(), errType) {
				return false
			}
		}
		if !k.constErrorCall(vs.Values[i]) {
			return false
		}
	}
	return true
}

// constErrorCall reports whether e is a call to errors.New or fmt.Errorf
// whose arguments are all constant expressions.
func (k *varKinds) constErrorCall(e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || call.Ellipsis.IsValid() {
		return false
	}
	switch path, name := k.callee(call.Fun); {
	case path == "errors" && name == "New", path == "fmt" && name == "Errorf":
	default:
		return false
	}
	for _, a := range call.Args {
		if !k.constant(a) {
			return false
		}
	}
	return true
}

// callee returns the package path and name of the function fun names, or
// empty strings when fun names no package-level function. It resolves fun
// through the type information and, for a tree that information does not
// cover, through the file's imports and the package scope.
func (k *varKinds) callee(fun ast.Expr) (path, name string) {
	var id *ast.Ident
	switch f := ast.Unparen(fun).(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	default:
		return "", ""
	}
	if k.info != nil {
		if obj, ok := k.info.Uses[id]; ok {
			fn, ok := obj.(*types.Func)
			if !ok || fn.Pkg() == nil {
				return "", ""
			}
			return fn.Pkg().Path(), fn.Name()
		}
	}
	switch f := ast.Unparen(fun).(type) {
	case *ast.Ident:
		if k.scope == nil {
			return "", ""
		}
		if _, ok := k.scope.Lookup(f.Name).(*types.Func); ok {
			return k.pkgPath, f.Name
		}
	case *ast.SelectorExpr:
		if x, ok := f.X.(*ast.Ident); ok {
			return importPath(k.file, x.Name), f.Sel.Name
		}
	}
	return "", ""
}

// importPath returns the path of the import of f bound to name, or "" when
// none is. An import without a name is bound to its path's last element,
// which is its package name for the standard-library packages this reads.
func importPath(f *ast.File, name string) string {
	for _, s := range f.Imports {
		path, err := strconv.Unquote(s.Path.Value)
		if err != nil {
			continue
		}
		local := path[strings.LastIndexByte(path, '/')+1:]
		if s.Name != nil {
			local = s.Name.Name
		}
		if local == name {
			return path
		}
	}
	return ""
}

// buildInfo reports whether every name of vs has a boolean, numeric or
// string basic type and every initializer of vs is constant.
func (k *varKinds) buildInfo(vs *ast.ValueSpec) bool {
	if k.scope == nil {
		return false
	}
	for _, v := range vs.Values {
		if !k.constant(v) {
			return false
		}
	}
	for _, n := range vs.Names {
		if n.Name == "_" {
			continue
		}
		v, ok := k.scope.Lookup(n.Name).(*types.Var)
		if !ok {
			return false
		}
		b, ok := types.Unalias(v.Type()).(*types.Basic)
		if !ok || b.Info()&(types.IsBoolean|types.IsNumeric|types.IsString) == 0 {
			return false
		}
	}
	return true
}

// constant reports whether e is a constant expression. It asks the type
// information and, for a tree that information does not cover, accepts
// literals, the package's and the universe's constants by name, and unary,
// binary and parenthesized expressions of those.
func (k *varKinds) constant(e ast.Expr) bool {
	if k.info != nil {
		if tv, ok := k.info.Types[e]; ok {
			return tv.Value != nil
		}
	}
	switch e := e.(type) {
	case *ast.BasicLit:
		return true
	case *ast.ParenExpr:
		return k.constant(e.X)
	case *ast.UnaryExpr:
		return e.Op != token.AND && e.Op != token.ARROW && k.constant(e.X)
	case *ast.BinaryExpr:
		return k.constant(e.X) && k.constant(e.Y)
	case *ast.Ident:
		obj := types.Universe.Lookup(e.Name)
		if k.scope != nil {
			if o := k.scope.Lookup(e.Name); o != nil {
				obj = o
			}
		}
		_, ok := obj.(*types.Const)
		return ok
	}
	return false
}
