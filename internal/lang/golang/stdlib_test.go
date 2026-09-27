package golang

import (
	"context"
	"errors"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// extractStdlibOrSkip extracts the standard-library package at importPath,
// skipping the test when the toolchain cannot load it.
func extractStdlibOrSkip(t *testing.T, importPath string, opts ...Option) metrics.RawMetrics {
	t.Helper()
	got, err := ExtractStdlib(t.Context(), importPath, opts...)
	if err != nil {
		t.Skipf("loading stdlib %s: %v", importPath, err)
	}
	return got
}

func TestExtractStdlib(t *testing.T) {
	got := extractStdlibOrSkip(t, "errors")
	if err := got.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
	if got.Globals != 2 || got.InternalImports != 0 || got.FanIn != 0 || !got.HasTests || got.TestFuncs <= 0 {
		t.Errorf("globals=%d internal_imports=%d fan_in=%d has_tests=%v test_funcs=%d, want 2, 0, 0, true, >0",
			got.Globals, got.InternalImports, got.FanIn, got.HasTests, got.TestFuncs)
	}
	if got.TokensEstWithTests <= got.TokensEst || got.ExportedSymbols == 0 {
		t.Errorf("tokens_est=%d tokens_est_with_tests=%d exported_symbols=%d, want test tokens and exports",
			got.TokensEst, got.TokensEstWithTests, got.ExportedSymbols)
	}
}

func TestExtractStdlibAppliesOptions(t *testing.T) {
	base := extractStdlibOrSkip(t, "errors").TokensEst
	halved := extractStdlibOrSkip(t, "errors", WithCharsPerToken(2*defaultCharsPerToken)).TokensEst
	// Doubling the ratio halves tokens_est, give or take the truncation.
	if d := base - 2*halved; d < 0 || d > 1 {
		t.Errorf("tokens_est = %d at ratio %v and %d at twice it, want half", base, defaultCharsPerToken, halved)
	}
}

func TestExtractStdlibFailures(t *testing.T) {
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	tests := []struct {
		name       string
		ctx        context.Context
		importPath string
		opts       []Option
		want       error // nil means any non-nil error
	}{
		{"unknown tokenizer", t.Context(), "errors", []Option{WithTokenizer("cl100k")}, ErrUnknownTokenizer},
		{"cancelled", cancelled, "errors", nil, context.Canceled},
		{"no such package", t.Context(), "astimate/no/such/package", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ExtractStdlib(tt.ctx, tt.importPath, tt.opts...)
			switch {
			case err == nil:
				t.Fatal("err = nil, want an error")
			case tt.want != nil && !errors.Is(err, tt.want):
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}
