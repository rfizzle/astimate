package inspect

import (
	"go/ast"
	"go/token"
)

// Function fingerprints (SPEC.md 6.5).
//
// A function's fingerprint is a 64-bit FNV-1a hash of its body's syntax
// tree, walked in the same ast.Inspect pass that measures nesting (see
// inspectBody). Each node
// contributes a code for its kind, with the operator or keyword token where
// the kind alone does not fix it (binary and unary operators, assignment and
// increment tokens, branch keywords, declaration keywords, channel
// directions), and each node's end contributes a close code, so the hash
// covers the tree's shape and every token of the body. Identifiers hash as
// one ID code and basic literals as one LIT code, the normalization of the
// duplication stream (SPEC.md 6.3). Comments and positions are not in the
// walk, so an edit to comments, layout or gofmt formatting leaves the
// fingerprint unchanged, and so does renaming a variable or changing a
// literal's value; any edit to control flow or operators changes it. The
// one name the hash sees is the enclosing function's own, on a direct call
// to it, because cognitive complexity counts direct recursion.

const (
	// fpOffset and fpPrime are the 64-bit FNV-1a parameters.
	fpOffset uint64 = 14695981039346656037
	fpPrime  uint64 = 1099511628211
	// fpClose is mixed in when the walk leaves a node.
	fpClose uint32 = 1
)

// fpMix folds v into the hash h as one FNV-1a step over a 32-bit word
// rather than four bytes: node codes are the units, and one step per code
// keeps the walk cheap.
func fpMix(h uint64, v uint32) uint64 {
	return (h ^ uint64(v)) * fpPrime
}

// Node kind codes, above fpClose. The values only need to be distinct and
// stable within one release, since baselines extracted by the same binary
// are compared; a baseline file written by an older release whose codes
// differ marks every function changed once, until it is rewritten.
const (
	fpIdent uint32 = iota + 2
	fpLit
	fpBad
	fpEllipsis
	fpFuncLit
	fpCompositeLit
	fpParen
	fpSelector
	fpIndex
	fpIndexList
	fpSlice
	fpTypeAssert
	fpCall
	fpStar
	fpUnary
	fpBinary
	fpKeyValue
	fpArrayType
	fpStructType
	fpFuncType
	fpInterfaceType
	fpMapType
	fpChanType
	fpDeclStmt
	fpEmptyStmt
	fpLabeled
	fpExprStmt
	fpSend
	fpIncDec
	fpAssign
	fpGo
	fpDefer
	fpReturn
	fpBranch
	fpBlock
	fpIf
	fpCaseClause
	fpSwitch
	fpTypeSwitch
	fpCommClause
	fpSelect
	fpFor
	fpRange
	fpField
	fpFieldList
	fpGenDecl
	fpValueSpec
	fpTypeSpec
	fpOther
	// fpSelfCall follows the code of a call whose function is an
	// identifier naming the enclosing function: gocognit scores direct
	// recursion, so renaming a call target to or from the function itself
	// changes complexity and must change the fingerprint.
	fpSelfCall
)

// fpCode returns the fingerprint code of node n: its kind in the low byte
// and, where the kind leaves it open, the token or the set of optional
// children in the bits above. It never returns 0 or fpClose.
func fpCode(n ast.Node) uint32 {
	with := func(kind uint32, tok token.Token) uint32 { return kind | uint32(tok)<<8 }
	switch x := n.(type) {
	case *ast.Ident:
		return fpIdent
	case *ast.BasicLit:
		return fpLit
	case *ast.BinaryExpr:
		return with(fpBinary, x.Op)
	case *ast.UnaryExpr:
		return with(fpUnary, x.Op)
	case *ast.AssignStmt:
		return with(fpAssign, x.Tok)
	case *ast.IncDecStmt:
		return with(fpIncDec, x.Tok)
	case *ast.BranchStmt:
		return with(fpBranch, x.Tok)
	case *ast.GenDecl:
		return with(fpGenDecl, x.Tok)
	case *ast.RangeStmt:
		return with(fpRange, x.Tok)
	case *ast.ChanType:
		return fpChanType | uint32(x.Dir)<<8
	case *ast.SliceExpr:
		return fpSlice | flags(x.Low != nil, x.High != nil, x.Max != nil, x.Slice3)
	case *ast.ForStmt:
		return fpFor | flags(x.Init != nil, x.Cond != nil, x.Post != nil, false)
	case *ast.IfStmt:
		return fpIf | flags(x.Init != nil, x.Else != nil, false, false)
	case *ast.SwitchStmt:
		return fpSwitch | flags(x.Init != nil, x.Tag != nil, false, false)
	case *ast.TypeSwitchStmt:
		return fpTypeSwitch | flags(x.Init != nil, false, false, false)
	case *ast.CaseClause:
		return fpCaseClause | flags(x.List == nil, false, false, false)
	case *ast.CommClause:
		return fpCommClause | flags(x.Comm == nil, false, false, false)
	case *ast.CallExpr:
		return fpCall | flags(x.Ellipsis.IsValid(), false, false, false)
	case *ast.TypeAssertExpr:
		return fpTypeAssert | flags(x.Type == nil, false, false, false)
	case *ast.ReturnStmt:
		return fpReturn
	case *ast.BlockStmt:
		return fpBlock
	case *ast.ExprStmt:
		return fpExprStmt
	case *ast.CompositeLit:
		return fpCompositeLit
	case *ast.FuncLit:
		return fpFuncLit
	case *ast.ParenExpr:
		return fpParen
	case *ast.SelectorExpr:
		return fpSelector
	case *ast.IndexExpr:
		return fpIndex
	case *ast.IndexListExpr:
		return fpIndexList
	case *ast.StarExpr:
		return fpStar
	case *ast.KeyValueExpr:
		return fpKeyValue
	case *ast.Ellipsis:
		return fpEllipsis
	case *ast.ArrayType:
		return fpArrayType
	case *ast.StructType:
		return fpStructType
	case *ast.FuncType:
		return fpFuncType
	case *ast.InterfaceType:
		return fpInterfaceType
	case *ast.MapType:
		return fpMapType
	case *ast.DeclStmt:
		return fpDeclStmt
	case *ast.EmptyStmt:
		return fpEmptyStmt
	case *ast.LabeledStmt:
		return fpLabeled
	case *ast.SendStmt:
		return fpSend
	case *ast.GoStmt:
		return fpGo
	case *ast.DeferStmt:
		return fpDefer
	case *ast.SelectStmt:
		return fpSelect
	case *ast.Field:
		return fpField
	case *ast.FieldList:
		return fpFieldList
	case *ast.ValueSpec:
		return fpValueSpec
	case *ast.TypeSpec:
		return fpTypeSpec | flags(x.Assign.IsValid(), false, false, false)
	case *ast.BadExpr, *ast.BadStmt, *ast.BadDecl:
		return fpBad
	default:
		return fpOther
	}
}

// flags packs up to four presence bits above a kind's low byte.
func flags(a, b, c, d bool) uint32 {
	var f uint32
	for i, set := range [4]bool{a, b, c, d} {
		if set {
			f |= 1 << (8 + i)
		}
	}
	return f
}
