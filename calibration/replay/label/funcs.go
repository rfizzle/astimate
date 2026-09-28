package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

// fileDiff is one file of a -U0 patch: its path on each side, empty when
// the file is absent there, and the line ranges each side lost or gained,
// as {first, count}.
type fileDiff struct {
	oldPath, newPath string
	old, new         [][2]int
	// inHunks is set at the first hunk, after which a line starting ---
	// or +++ is content, not a header.
	inHunks bool
}

// parseDiff parses git's -U0 patch output into its files.
func parseDiff(out []byte) []fileDiff {
	var files []fileDiff
	var cur *fileDiff
	for line := range strings.Lines(string(out)) {
		line = strings.TrimSuffix(line, "\n")
		switch {
		case strings.HasPrefix(line, "diff --git "):
			files = append(files, fileDiff{})
			cur = &files[len(files)-1]
		case cur == nil:
		case strings.HasPrefix(line, "@@ -"):
			cur.inHunks = true
			if o, n, ok := parseHunk(line); ok {
				cur.old = appendRange(cur.old, o)
				cur.new = appendRange(cur.new, n)
			}
		case cur.inHunks:
		case strings.HasPrefix(line, "--- "):
			cur.oldPath = diffPath(line[4:], "a/")
		case strings.HasPrefix(line, "+++ "):
			cur.newPath = diffPath(line[4:], "b/")
		}
	}
	return files
}

// diffPath returns the path of a ---/+++ header, without its a/ or b/
// prefix; empty for /dev/null.
func diffPath(s, prefix string) string {
	if s == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(s, prefix)
}

// appendRange appends r to rs unless it covers no line.
func appendRange(rs [][2]int, r [2]int) [][2]int {
	if r[1] == 0 {
		return rs
	}
	return append(rs, r)
}

// parseHunk parses "@@ -a[,b] +c[,d] @@ ..." into {a, b} and {c, d}; a
// missing count is 1.
func parseHunk(line string) (o, n [2]int, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return o, n, false
	}
	o, okO := parseRange(strings.TrimPrefix(fields[1], "-"))
	n, okN := parseRange(strings.TrimPrefix(fields[2], "+"))
	return o, n, okO && okN
}

// parseRange parses "a[,b]".
func parseRange(s string) ([2]int, bool) {
	first, count, hasCount := strings.Cut(s, ",")
	a, err := strconv.Atoi(first)
	if err != nil {
		return [2]int{}, false
	}
	b := 1
	if hasCount {
		if b, err = strconv.Atoi(count); err != nil {
			return [2]int{}, false
		}
	}
	return [2]int{a, b}, true
}

// funcsAt returns the functions of the Go source src whose declaration,
// doc comment excluded, overlaps one of lines ({first, count} ranges), as
// Name or Receiver.Name, plus the package-level variables whose function
// literals overlap one (literalsHit); nil when src does not parse.
func funcsAt(src []byte, lines [][2]int) []string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	var names []string
	hit := func(n ast.Node) bool {
		start, end := fset.Position(n.Pos()).Line, fset.Position(n.End()).Line
		return slices.ContainsFunc(lines, func(r [2]int) bool { return r[0] <= end && r[0]+r[1]-1 >= start })
	}
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if hit(d) {
				names = append(names, funcName(d))
			}
		case *ast.GenDecl:
			names = append(names, literalsHit(d, hit)...)
		}
	}
	return names
}

// literalsHit returns, for each package-level variable of d whose value
// holds a function literal that hit reports, the name "var <name>": a
// cobra command's RunE, a handler table entry. Edits elsewhere in the
// value, such as a usage string, do not count.
func literalsHit(d *ast.GenDecl, hit func(ast.Node) bool) []string {
	var names []string
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok || len(vs.Names) == 0 {
			continue
		}
		found := false
		for _, v := range vs.Values {
			ast.Inspect(v, func(n ast.Node) bool {
				if lit, ok := n.(*ast.FuncLit); ok && !found {
					found = hit(lit)
					return false
				}
				return !found
			})
		}
		if found {
			names = append(names, "var "+vs.Names[0].Name)
		}
	}
	return names
}

// funcName returns fd's name, prefixed with its receiver's base type name
// and a dot for a method, as metrics.FunctionInfo.QualifiedName does.
func funcName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	recv := strings.TrimLeft(types.ExprString(fd.Recv.List[0].Type), "*(")
	if k := strings.IndexAny(recv, "[)"); k >= 0 {
		recv = recv[:k]
	}
	return recv + "." + fd.Name.Name
}
