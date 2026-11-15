package surface

import (
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

func TestReshaped(t *testing.T) {
	t.Parallel()

	fn := func(recv, name string, sig uint64, file string, line int) metrics.FunctionInfo {
		return metrics.FunctionInfo{Receiver: recv, Name: name, Signature: sig, File: file, Line: line}
	}
	sym := func(kind, recv, name, file string, line int) metrics.Symbol {
		return metrics.Symbol{Kind: kind, Receiver: recv, Name: name, Position: metrics.Position{File: file, Line: line}}
	}
	tests := []struct {
		name        string
		head        metrics.Details
		baseFns     []metrics.FunctionInfo
		baseExports []metrics.Symbol
		want        []string
		at          metrics.Position
	}{
		{
			name: "signature changes sorted, then unexported",
			head: metrics.Details{
				Functions:  []metrics.FunctionInfo{fn("Store", "SetLatest", 2, "s.go", 9), fn("", "Open", 4, "o.go", 3), fn("", "Same", 5, "o.go", 7)},
				Exports:    []metrics.Symbol{sym("func", "", "Open", "o.go", 3)},
				Unexported: []metrics.Symbol{sym("type", "", "options", "t.go", 1)},
			},
			baseFns:     []metrics.FunctionInfo{fn("Store", "SetLatest", 1, "", 0), fn("", "Open", 3, "", 0), fn("", "Same", 5, "", 0)},
			baseExports: []metrics.Symbol{sym("func", "", "Open", "", 0), sym("type", "", "Options", "", 0)},
			want:        []string{"Open (signature changed)", "Store.SetLatest (signature changed)", "Options (unexported as options)"},
			at:          metrics.Position{File: "o.go", Line: 3},
		},
		{
			name:    "baseline functions without signatures",
			head:    metrics.Details{Functions: []metrics.FunctionInfo{fn("", "Open", 4, "o.go", 3)}},
			baseFns: []metrics.FunctionInfo{fn("", "Open", 0, "", 0)},
		},
		{
			name:    "new function",
			head:    metrics.Details{Functions: []metrics.FunctionInfo{fn("", "Open", 4, "o.go", 3)}},
			baseFns: []metrics.FunctionInfo{fn("", "Other", 4, "", 0)},
		},
		{
			name:        "initialism and method on the same receiver",
			head:        metrics.Details{Unexported: []metrics.Symbol{sym("var", "", "httpClient", "c.go", 2), sym("func", "T", "run", "t.go", 5)}},
			baseExports: []metrics.Symbol{sym("var", "", "HTTPClient", "", 0), sym("func", "T", "Run", "", 0)},
			want:        []string{"HTTPClient (unexported as httpClient)", "T.Run (unexported as run)"},
			at:          metrics.Position{File: "c.go", Line: 2},
		},
		{
			name:        "another kind of the lowered name",
			head:        metrics.Details{Unexported: []metrics.Symbol{sym("var", "", "config", "c.go", 2)}},
			baseExports: []metrics.Symbol{sym("type", "", "Config", "", 0)},
		},
		{
			name:        "method moved to another receiver",
			head:        metrics.Details{Unexported: []metrics.Symbol{sym("func", "U", "run", "t.go", 5)}},
			baseExports: []metrics.Symbol{sym("func", "T", "Run", "", 0)},
		},
		{
			name: "still exported",
			head: metrics.Details{Exports: []metrics.Symbol{sym("type", "", "Options", "o.go", 1)},
				Unexported: []metrics.Symbol{sym("func", "", "options", "o.go", 4)}},
			baseExports: []metrics.Symbol{sym("type", "", "Options", "", 0)},
		},
		{
			name:        "deleted",
			baseExports: []metrics.Symbol{sym("type", "", "Options", "", 0)},
		},
		{
			name: "baseline recorded neither",
			head: metrics.Details{Functions: []metrics.FunctionInfo{fn("", "Open", 4, "o.go", 3)},
				Unexported: []metrics.Symbol{sym("type", "", "options", "o.go", 1)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			names, at := Reshaped(&tt.head, tt.baseFns, tt.baseExports)
			if !slices.Equal(names, tt.want) || at != tt.at {
				t.Errorf("Reshaped = %q at %+v, want %q at %+v", names, at, tt.want, tt.at)
			}
		})
	}
}
