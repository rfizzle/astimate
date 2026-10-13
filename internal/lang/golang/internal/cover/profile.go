package cover

import (
	"fmt"
	"path"
	"strconv"
	"strings"
)

// Pos is a place in a source file as a coverage profile writes it: a
// 1-based line and a 1-based column in bytes.
type Pos struct {
	Line, Col int
}

// before reports whether p is strictly before q.
func (p Pos) before(q Pos) bool {
	return p.Line < q.Line || (p.Line == q.Line && p.Col < q.Col)
}

// span is one block of a profile: the range of source it covers.
type span struct {
	start, end Pos
}

// block is what a profile records for a span: its statement count and how
// often it ran, summed over the profiles merged into it.
type block struct {
	stmts, count int
}

// Profile is the statement blocks of one or more `go test -coverprofile`
// files, keyed by the file name the profile writes, the package's import
// path joined with the file's base name. Merging several adds up the run
// counts of a block they share, so a block is covered when any run
// executed it. The zero value is an empty profile; use Merge to fill it.
type Profile struct {
	files map[string]map[span]block
}

// ParseProfile parses the text of a coverage profile: a "mode:" line and
// then one line per block, "file:startLine.startCol,endLine.endCol
// numStmts count". Blank lines are skipped. A malformed line is an error
// naming its line number.
func ParseProfile(data []byte) (Profile, error) {
	var p Profile
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		file, s, b, err := parseBlock(line)
		if err != nil {
			return Profile{}, fmt.Errorf("coverage profile line %d: %w", i+1, err)
		}
		p.add(file, s, b)
	}
	return p, nil
}

// parseBlock parses one block line of a profile.
func parseBlock(line string) (string, span, block, error) {
	// The file name may hold a colon (a Windows path), so cut at the last.
	i := strings.LastIndexByte(line, ':')
	if i < 0 {
		return "", span{}, block{}, fmt.Errorf("no file name in %q", line)
	}
	file, rest := line[:i], line[i+1:]
	f := strings.Fields(rest)
	if len(f) != 3 {
		return "", span{}, block{}, fmt.Errorf("want range, statements and count in %q", line)
	}
	from, to, ok := strings.Cut(f[0], ",")
	if !ok {
		return "", span{}, block{}, fmt.Errorf("no range in %q", line)
	}
	start, err1 := parsePos(from)
	end, err2 := parsePos(to)
	stmts, err3 := strconv.Atoi(f[1])
	count, err4 := strconv.Atoi(f[2])
	for _, err := range []error{err1, err2, err3, err4} {
		if err != nil {
			return "", span{}, block{}, fmt.Errorf("parsing %q: %w", line, err)
		}
	}
	return file, span{start: start, end: end}, block{stmts: stmts, count: count}, nil
}

// parsePos parses "line.col".
func parsePos(s string) (Pos, error) {
	l, c, ok := strings.Cut(s, ".")
	if !ok {
		return Pos{}, fmt.Errorf("position %q is not line.col", s)
	}
	line, err := strconv.Atoi(l)
	if err != nil {
		return Pos{}, err
	}
	col, err := strconv.Atoi(c)
	if err != nil {
		return Pos{}, err
	}
	return Pos{Line: line, Col: col}, nil
}

// add records b for span s of file, adding its count to a block already
// recorded there.
func (p *Profile) add(file string, s span, b block) {
	if p.files == nil {
		p.files = make(map[string]map[span]block)
	}
	blocks := p.files[file]
	if blocks == nil {
		blocks = make(map[span]block)
		p.files[file] = blocks
	}
	if old, ok := blocks[s]; ok {
		b.count += old.count
	}
	blocks[s] = b
}

// Merge adds every block of q to p.
func (p *Profile) Merge(q Profile) {
	for file, blocks := range q.files {
		for s, b := range blocks {
			p.add(file, s, b)
		}
	}
}

// Pct returns the statement coverage of the package with import path pkg,
// the share of the statements in its own files that ran, as go test -cover
// prints it: a percentage rounded to one decimal. It is nil when the
// profile has no statement of the package.
func (p Profile) Pct(pkg string) *float64 {
	total, covered := 0, 0
	for file, blocks := range p.files {
		if path.Dir(file) != pkg {
			continue
		}
		for _, b := range blocks {
			total += b.stmts
			if b.count > 0 {
				covered += b.stmts
			}
		}
	}
	if total == 0 {
		return nil
	}
	v, err := strconv.ParseFloat(strconv.FormatFloat(100*float64(covered)/float64(total), 'f', 1, 64), 64)
	if err != nil {
		return nil
	}
	return &v
}

// Has reports whether the profile has a block of a file of the package
// with import path pkg, that is, whether a run instrumented the package.
func (p Profile) Has(pkg string) bool {
	for file := range p.files {
		if path.Dir(file) == pkg {
			return true
		}
	}
	return false
}

// Ran reports whether a block of file that holds a statement and lies
// within from and to, inclusive, ran: the test of SPEC.md 6.4 for an
// export whose body is that range. file is the name the profile writes,
// the package's import path joined with the file's base name.
func (p Profile) Ran(file string, from, to Pos) bool {
	for s, b := range p.files[file] {
		if b.stmts > 0 && b.count > 0 && !s.start.before(from) && !to.before(s.end) {
			return true
		}
	}
	return false
}
