package inspect

import (
	"go/token"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"github.com/rfizzle/astimate/internal/metrics"
)

// Positions resolves each of ps in fset to a file relative to dir and a
// line, honoring //line directives, so a cgo package's rewritten sources
// map back to the files cgo read. It returns nil for no positions, and an
// invalid position, or a nil fset, yields an empty Position.
func Positions(fset *token.FileSet, dir string, ps []token.Pos) []metrics.Position {
	if len(ps) == 0 {
		return nil
	}
	out := make([]metrics.Position, len(ps))
	for i, p := range ps {
		out[i] = position(fset, dir, p)
	}
	return out
}

// position resolves p as Positions does each of its positions.
func position(fset *token.FileSet, dir string, p token.Pos) metrics.Position {
	if fset == nil || !p.IsValid() {
		return metrics.Position{}
	}
	pos := fset.Position(p)
	return metrics.Position{File: load.RelFile(dir, pos.Filename), Line: pos.Line}
}

// FunctionInfos converts fcs, the functions of a package in dir, to
// metrics.FunctionInfo, resolving each position in fset as Positions
// does. A nil fset leaves File and Line empty.
func FunctionInfos(fset *token.FileSet, dir string, fcs []Func) []metrics.FunctionInfo {
	fns := make([]metrics.FunctionInfo, len(fcs))
	for i, fc := range fcs {
		pos := position(fset, dir, fc.Pos)
		fns[i] = metrics.FunctionInfo{
			Receiver:    fc.Receiver,
			Name:        fc.Ident,
			Fingerprint: fc.Fingerprint,
			Signature:   fc.Signature,
			Cognitive:   fc.Cognitive,
			Cyclomatic:  fc.Cyclomatic,
			File:        pos.File,
			Line:        pos.Line,
		}
	}
	return fns
}

// Symbols converts ss, declarations of a package in dir, to
// metrics.Symbol, resolving each position in fset as Positions does; nil
// for none.
func Symbols(fset *token.FileSet, dir string, ss []Symbol) []metrics.Symbol {
	if len(ss) == 0 {
		return nil
	}
	out := make([]metrics.Symbol, len(ss))
	for i, s := range ss {
		out[i] = metrics.Symbol{Kind: s.Kind, Receiver: s.Receiver, Name: s.Name, Position: position(fset, dir, s.Pos)}
	}
	return out
}
