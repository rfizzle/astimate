package narrate

import (
	"strconv"

	"github.com/rfizzle/astimate/internal/report"
)

// HookNotes gives the Stop hook's violation lines the report's sentences:
// for a violation of a package, the why sentence the change report prints
// under it and its place to open ("file:line", the file alone without a
// line) (report.WriteHookNotes). A violation the report has no finding
// for gets neither.
func (m *Module) HookNotes() report.FindingNotes {
	type key struct{ path, metric string }
	type note struct{ why, open string }
	notes := map[key]note{}
	if m.Change != nil {
		for _, f := range m.Change.Failing {
			for _, x := range f.Findings {
				notes[key{f.Path, x.Metric}] = note{x.Why, place(x.Location)}
			}
		}
	}
	return func(path, metric string) (string, string) {
		n := notes[key{path, metric}]
		return n.why, n.open
	}
}

// place renders l as "file:line", or the file alone without a line; empty
// for no location.
func place(l *report.Location) string {
	switch {
	case l == nil:
		return ""
	case l.Line > 0:
		return l.File + ":" + strconv.Itoa(l.Line)
	}
	return l.File
}
