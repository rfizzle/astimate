package golang

import (
	"go/ast"
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
	// cognitive is the gocognit score.
	cognitive int
	// nesting is the deepest nesting inside the function body.
	nesting int
}

// complexity measures the nesting depth and cognitive complexity of every
// top-level function and method in p's non-test files, init functions
// included (SPEC.md 6.5). Function literals are scored as part of the
// function that contains them.
func complexity(l *loaded, p *packages.Package) complexityCounts {
	var c complexityCounts
	for _, f := range sourceSyntax(l, p) {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			fc := funcComplexity{
				name:      funcDeclName(fn),
				cognitive: gocognit.Complexity(fn),
				nesting:   nesting(fn.Body),
			}
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

// nesting returns the deepest stack of if, for, range, switch, type switch,
// select and func literal nodes inside body, which is itself depth 0. Case
// clauses and else blocks add nothing; an else if is an IfStmt inside the
// outer one, so it adds a level. A func literal adds a level and its body
// continues from the enclosing depth, so nesting inside it counts relative to
// the enclosing function.
func nesting(body *ast.BlockStmt) int {
	if body == nil {
		return 0
	}
	var (
		depth, deepest int
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
			return true
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
	return deepest
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

// funcDeclName returns fn's name, prefixed with its receiver type name and a
// dot for methods.
func funcDeclName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
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
			return x.Name + "." + fn.Name.Name
		default:
			return fn.Name.Name
		}
	}
}
