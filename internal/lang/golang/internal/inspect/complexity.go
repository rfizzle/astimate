// Package inspect measures the syntax of one Go package: size, exported
// symbols, globals and init functions, control-flow complexity with each
// function's fingerprint, the opacity flags, and token counts (SPEC.md
// section 6). Each function body is walked by one ast.Inspect call that
// measures its nesting, hashes it and records the package-level variables
// it writes together (see fingerprint.go and writes.go); package-level var
// initializers are scanned for writes once, and the other metrics read only
// the top-level declarations, import specs and file bytes.
package inspect

import (
	"go/ast"
	"go/token"
	"iter"
	"slices"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"github.com/uudashr/gocognit"
	"golang.org/x/tools/go/packages"
)

// ComplexityCounts is the control-flow shape of one package: how deep its
// functions nest and how hard they are to follow.
type ComplexityCounts struct {
	// MaxNesting is the deepest nesting over all functions; 0 with none.
	MaxNesting int
	// CognitiveTotal is the sum of per-function cognitive complexity.
	CognitiveTotal int
	// CognitiveP90 is the nearest-rank 90th percentile of per-function
	// cognitive complexity; 0 with no functions.
	CognitiveP90 int
	// FuncCount is the number of top-level funcs and methods, init included.
	FuncCount int
	// PerFunc holds each function's scores in declaration order.
	PerFunc []Func
	// Written holds the name of every package-level variable that a
	// function body or package-level var initializer of the authored files
	// assigns, increments, decrements or takes the address of; nil when
	// none does. Globals reads it (SPEC.md 6.5). Where type information
	// does not resolve an identifier, the name is recorded whatever it
	// denotes.
	Written map[string]bool
}

// Func is the complexity of one top-level function or method.
type Func struct {
	// Name is the function name, prefixed with the receiver type and a dot
	// for methods.
	Name string
	// Receiver is the method's receiver base type name, empty for a plain
	// function; Ident is the bare function name.
	Receiver, Ident string
	// Cognitive is the gocognit score.
	Cognitive int
	// Nesting is the deepest nesting inside the function body.
	Nesting int
	// Fingerprint hashes the body's normalized syntax (see fingerprint.go).
	Fingerprint uint64
	// Pos is the declaration's position in the load's file set, resolved
	// to a file and line only when a caller asks for it.
	Pos token.Pos
}

// Complexity measures the nesting depth and cognitive complexity of every
// top-level function and method in p's authored non-test files, init
// functions included and generated files left out (SPEC.md 6.5; see
// load.Module.AuthoredSyntax), so the function list
// changed_func_cognitive_max diffs holds no generated function either, and
// fingerprints each body in the same walk that measures its nesting.
// Function literals are scored as part of the function that contains them.
// The same walk records the package-level variables each body writes, and
// the package-level var initializers, which no other metric walks, are
// scanned for writes too (ComplexityCounts.Written).
func Complexity(m *load.Module, p *packages.Package) ComplexityCounts {
	var c ComplexityCounts
	w := newWrites(p)
	for d := range decls(m, p) {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			w.noteInitializers(d)
			continue
		}
		recv := funcReceiver(fn)
		fc := Func{
			Name:      qualify(recv, fn.Name.Name),
			Receiver:  recv,
			Ident:     fn.Name.Name,
			Cognitive: gocognit.Complexity(fn),
			Pos:       fn.Pos(),
		}
		fc.Nesting, fc.Fingerprint = inspectBody(fn.Name.Name, fn.Body, w)
		c.PerFunc = append(c.PerFunc, fc)
		c.CognitiveTotal += fc.Cognitive
		c.MaxNesting = max(c.MaxNesting, fc.Nesting)
	}
	c.Written = w.names
	c.FuncCount = len(c.PerFunc)
	scores := make([]int, len(c.PerFunc))
	for i, fc := range c.PerFunc {
		scores[i] = fc.Cognitive
	}
	c.CognitiveP90 = p90(scores)
	return c
}

// decls yields the top-level declarations of p's authored non-test files
// (load.Module.AuthoredSyntax), in file and declaration order.
func decls(m *load.Module, p *packages.Package) iter.Seq[ast.Decl] {
	return func(yield func(ast.Decl) bool) {
		for _, f := range m.AuthoredSyntax(p) {
			for _, d := range f.Decls {
				if !yield(d) {
					return
				}
			}
		}
	}
}

// inspectBody walks body once and returns its nesting and its fingerprint.
//
// Nesting is the deepest stack of if, for, range, switch, type switch,
// select and func literal nodes inside body, which is itself depth 0. Case
// clauses and else blocks add nothing; an else if is an IfStmt inside the
// outer one, so it adds a level. A func literal adds a level and its body
// continues from the enclosing depth, so nesting inside it counts relative to
// the enclosing function.
//
// The fingerprint mixes every node's code on entry and a close code on
// exit, skipping comment groups, and marks direct calls to name, the
// enclosing function, as recursion (see fingerprint.go). A nil body, a
// function declared without one, has nesting 0 and the fingerprint of an
// empty walk.
//
// Each assignment, increment or decrement, address-of expression and range
// statement is also passed to w, which records the package-level variables
// the body writes; w may be nil.
func inspectBody(name string, body *ast.BlockStmt, w *writes) (deepest int, hash uint64) {
	hash = fpOffset
	if body == nil {
		return 0, hash
	}
	var (
		depth int
		// pushed records, for each node on the inspection stack, whether
		// entering it added a level.
		pushed []bool
	)
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			if pushed[len(pushed)-1] {
				depth--
			}
			pushed = pushed[:len(pushed)-1]
			hash = fpMix(hash, fpClose)
			return true
		}
		if _, ok := n.(*ast.CommentGroup); ok {
			return false
		}
		hash = fpMix(hash, fpCode(n))
		nests := false
		switch x := n.(type) {
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && id.Name == name {
				hash = fpMix(hash, fpSelfCall)
			}
		case *ast.RangeStmt:
			nests = true
			w.note(x)
		case *ast.IfStmt, *ast.ForStmt, *ast.SwitchStmt,
			*ast.TypeSwitchStmt, *ast.SelectStmt, *ast.FuncLit:
			nests = true
		case *ast.AssignStmt, *ast.IncDecStmt, *ast.UnaryExpr:
			w.note(x)
		}
		if nests {
			depth++
			deepest = max(deepest, depth)
		}
		pushed = append(pushed, nests)
		return true
	})
	return deepest, hash
}

// p90 returns the nearest-rank 90th percentile of scores: the value at 1-based
// rank ceil(0.9 * n) after sorting ascending, or 0 when scores is empty. It
// sorts scores in place.
func p90(scores []int) int {
	n := len(scores)
	if n == 0 {
		return 0
	}
	slices.Sort(scores)
	// ceil(9n / 10) in integer arithmetic.
	rank := (9*n + 9) / 10
	return scores[rank-1]
}

// qualify returns name, prefixed with receiver and a dot when receiver is
// not empty.
func qualify(receiver, name string) string {
	if receiver == "" {
		return name
	}
	return receiver + "." + name
}

// funcReceiver returns the base type name of fn's receiver, without a
// pointer, type parameters or parentheses, or "" for a plain function or a
// receiver whose type has no name.
func funcReceiver(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	t := fn.Recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.IndexExpr:
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		case *ast.ParenExpr:
			t = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}
