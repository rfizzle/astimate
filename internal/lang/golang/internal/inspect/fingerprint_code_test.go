package inspect

import (
	"go/ast"
	"go/token"
	"testing"
)

// TestFingerprintCode checks that node kinds get distinct codes, never 0 or
// fpClose, that the token or optional children a kind leaves open change
// the code, and that the code never depends on names, values or positions.
func TestFingerprintCode(t *testing.T) {
	x, y := ast.NewIdent("x"), ast.NewIdent("y")
	nodes := map[string]ast.Node{
		"ident":        x,
		"literal":      &ast.BasicLit{Kind: token.INT, Value: "1"},
		"add":          &ast.BinaryExpr{X: x, Op: token.ADD, Y: y},
		"sub":          &ast.BinaryExpr{X: x, Op: token.SUB, Y: y},
		"slice a[i:]":  &ast.SliceExpr{X: x, Low: y},
		"slice a[:i]":  &ast.SliceExpr{X: x, High: y},
		"send chan":    &ast.ChanType{Dir: ast.SEND, Value: x},
		"receive chan": &ast.ChanType{Dir: ast.RECV, Value: x},
		"return":       &ast.ReturnStmt{},
		"block":        &ast.BlockStmt{},
		"type alias":   &ast.TypeSpec{Name: x, Assign: 1, Type: y},
		"type def":     &ast.TypeSpec{Name: x, Type: y},
		"bad":          &ast.BadExpr{},
		"comment":      &ast.Comment{},
	}
	seen := make(map[uint32]string, len(nodes))
	for name, n := range nodes {
		c := fpCode(n)
		if c == 0 || c == fpClose {
			t.Errorf("fpCode(%s) = %d, want neither 0 nor fpClose", name, c)
		}
		if other, ok := seen[c]; ok {
			t.Errorf("fpCode(%s) = fpCode(%s) = %d, want them distinct", name, other, c)
		}
		seen[c] = name
	}
	renamed := &ast.BinaryExpr{X: ast.NewIdent("a"), OpPos: 42, Op: token.ADD, Y: &ast.BasicLit{Kind: token.STRING, Value: `"s"`}}
	if fpCode(renamed) != fpCode(nodes["add"]) {
		t.Error("Code of an addition depends on its operands' names or position")
	}
}

// TestFingerprintMix checks that fpMix is one FNV-1a step: order matters, and mixing
// from fpOffset is deterministic.
func TestFingerprintMix(t *testing.T) {
	a := fpMix(fpMix(fpOffset, 2), 3)
	b := fpMix(fpMix(fpOffset, 3), 2)
	if a == b {
		t.Error("Mix is insensitive to order")
	}
	if a != fpMix(fpMix(fpOffset, 2), 3) {
		t.Error("Mix is not deterministic")
	}
	h, v, p := fpOffset, uint64(fpSelfCall), fpPrime
	if got, want := fpMix(fpOffset, fpSelfCall), (h^v)*p; got != want {
		t.Errorf("fpMix(fpOffset, fpSelfCall) = %d, want %d", got, want)
	}
}
