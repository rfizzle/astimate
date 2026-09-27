package golang

import (
	"go/ast"
	"go/token"
	"slices"

	"github.com/uudashr/gocognit"
	"golang.org/x/tools/go/packages"
)

// complexityCounts is the control-flow shape of one package: how deep its
// functions nest and how hard they are to follow.
type complexityCounts struct {
	// maxNesting is the deepest nesting over all functions; 0 with none.
	maxNesting int
	// cognitiveTotal is the sum of per-function cognitive complexity.
	cognitiveTotal int
	// cognitiveP90 is the nearest-rank 90th percentile of per-function
	// cognitive complexity; 0 with no functions.
	cognitiveP90 int
	// funcCount is the number of top-level funcs and methods, init included.
	funcCount int
	// perFunc holds each function's scores in declaration order.
	perFunc []funcComplexity
}

// funcComplexity is the complexity of one top-level function or method.
type funcComplexity struct {
	// name is the function name, prefixed with the receiver type and a dot
	// for methods.
	name string
	// receiver is the method's receiver base type name, empty for a plain
	// function; ident is the bare function name.
	receiver, ident string
	// cognitive is the gocognit score.
	cognitive int
	// nesting is the deepest nesting inside the function body.
	nesting int
	// fingerprint hashes the body's normalized syntax (see fingerprint.go).
	fingerprint uint64
	// pos is the declaration's position in the load's file set, resolved
	// to a file and line only when Functions asks for it.
	pos token.Pos
}

// complexity measures the nesting depth and cognitive complexity of every
// top-level function and method in p's non-test files, init functions
// included (SPEC.md 6.5), and fingerprints each body in the same walk that
// measures its nesting. Function literals are scored as part of the
// function that contains them.
func complexity(l *loaded, p *packages.Package) complexityCounts {
	var c complexityCounts
	for _, f := range sourceSyntax(l, p) {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			recv := funcReceiver(fn)
			fc := funcComplexity{
				name:      qualify(recv, fn.Name.Name),
				receiver:  recv,
				ident:     fn.Name.Name,
				cognitive: gocognit.Complexity(fn),
				pos:       fn.Pos(),
			}
			fc.nesting, fc.fingerprint = inspectBody(fn.Name.Name, fn.Body)
			c.perFunc = append(c.perFunc, fc)
			c.cognitiveTotal += fc.cognitive
			c.maxNesting = max(c.maxNesting, fc.nesting)
		}
	}
	c.funcCount = len(c.perFunc)
	scores := make([]int, len(c.perFunc))
	for i, fc := range c.perFunc {
		scores[i] = fc.cognitive
	}
	c.cognitiveP90 = p90(scores)
	return c
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
func inspectBody(name string, body *ast.BlockStmt) (deepest int, fingerprint uint64) {
	fingerprint = fpOffset
	if body == nil {
		return 0, fingerprint
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
			fingerprint = fpMix(fingerprint, fpClose)
			return true
		}
		if _, ok := n.(*ast.CommentGroup); ok {
			return false
		}
		fingerprint = fpMix(fingerprint, fpCode(n))
		if call, ok := n.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == name {
				fingerprint = fpMix(fingerprint, fpSelfCall)
			}
		}
		nests := false
		switch n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt,
			*ast.TypeSwitchStmt, *ast.SelectStmt, *ast.FuncLit:
			nests = true
			depth++
			deepest = max(deepest, depth)
		}
		pushed = append(pushed, nests)
		return true
	})
	return deepest, fingerprint
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
