package compose

import (
	"reflect"
	"testing"

	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/report"
)

// TestNotAnalysed checks the packages not analysed: taken from the input,
// or from the check without them, sorted by path with the line counting
// them and each reason, the largest group first, and counted on the
// change; nothing at all when every package was analysed.
func TestNotAnalysed(t *testing.T) {
	t.Parallel()

	const constraints = "build constraints exclude all Go files"
	skipped := []report.Skipped{
		{Package: "tools/gen", Reason: constraints},
		{Package: "remote", Reason: "missing module", Error: "no required module", Selected: true},
		{Package: "cmd/gen", Reason: constraints},
	}
	want := []narrate.Skipped{{Path: "cmd/gen", Reason: constraints},
		{Path: "remote", Reason: "missing module", Error: "no required module"}, {Path: "tools/gen", Reason: constraints}}
	const line = "3 packages not analysed · build constraints exclude all Go files (2) · missing module (1)"

	for name, in := range map[string]*Input{
		"input": {Skipped: skipped},
		"check": {Check: &report.Check{Skipped: skipped}},
	} {
		m := Build(in)
		if !reflect.DeepEqual(m.Skipped, want) || m.NotAnalysed != line {
			t.Errorf("%s: Skipped = %+v, NotAnalysed = %q; want %+v, %q", name, m.Skipped, m.NotAnalysed, want, line)
		}
		if m.Change != nil && m.Change.NotAnalysed != 3 {
			t.Errorf("%s: Change.NotAnalysed = %d, want 3", name, m.Change.NotAnalysed)
		}
	}
	one := Build(&Input{Skipped: skipped[:1]})
	if one.NotAnalysed != "1 package not analysed · build constraints exclude all Go files (1)" {
		t.Errorf("one package: NotAnalysed = %q", one.NotAnalysed)
	}
	none := Build(&Input{Check: &report.Check{}})
	if none.Skipped != nil || none.NotAnalysed != "" || none.Change.NotAnalysed != 0 {
		t.Errorf("nothing skipped: Skipped = %+v, NotAnalysed = %q, Change.NotAnalysed = %d",
			none.Skipped, none.NotAnalysed, none.Change.NotAnalysed)
	}
}
