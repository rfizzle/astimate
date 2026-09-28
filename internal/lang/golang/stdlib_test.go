package golang

import (
	"context"
	"errors"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/inspect"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
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

// TestExtractStdlibUnsafe checks that the single-package path parses source
// files the way ExtractStdlibAll does: go/packages gives unsafe no syntax,
// so without that it would report no lines, exports or functions.
func TestExtractStdlibUnsafe(t *testing.T) {
	got := extractStdlibOrSkip(t, "unsafe")
	if got.Files != 1 || got.SLOC == 0 || got.ExportedSymbols == 0 || got.FuncCount == 0 {
		t.Errorf("files=%d sloc=%d exported_symbols=%d func_count=%d, want 1 file with lines, exports and functions",
			got.Files, got.SLOC, got.ExportedSymbols, got.FuncCount)
	}
}

func TestExtractStdlibAppliesOptions(t *testing.T) {
	base := extractStdlibOrSkip(t, "errors").TokensEst
	halved := extractStdlibOrSkip(t, "errors", WithCharsPerToken(2*inspect.DefaultCharsPerToken)).TokensEst
	// Doubling the ratio halves tokens_est, give or take the truncation.
	if d := base - 2*halved; d < 0 || d > 1 {
		t.Errorf("tokens_est = %d at ratio %v and %d at twice it, want half", base, inspect.DefaultCharsPerToken, halved)
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
		{"unknown tokenizer", t.Context(), "errors", []Option{WithTokenizer("cl100k")}, metrics.ErrUnknownTokenizer},
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

// TestExtractStdlibAllFanIn loads the whole standard library in one pass
// and checks that fan_in is real: packages imported across the library have
// a large fan_in, every standard-library import counts as internal so the
// module-wide sums of fan_in and internal_imports agree, and net/http keeps
// the PARTITION tier SPEC.md 7.5 pins it to under the default configuration.
func TestExtractStdlibAllFanIn(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the whole standard library")
	}
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	got, failed, err := ExtractStdlibAll(t.Context(),
		WithCharsPerToken(cfg.CharsPerToken),
		WithDupMinTokens(cfg.Duplication.MinTokens),
		WithDupIgnoreLiteralOnly(cfg.Duplication.IgnoreLiteralOnly),
		WithDupFoldSigns(cfg.Duplication.FoldSigns))
	if err != nil {
		t.Skipf("loading std: %v", err)
	}
	for path, err := range failed {
		t.Errorf("%s failed: %v", path, err)
	}

	const floor = 50
	for _, path := range []string{"errors", "fmt", "io"} {
		t.Run(path, func(t *testing.T) {
			m, ok := got[path]
			switch {
			case !ok:
				t.Fatalf("%s missing", path)
			case m.FanIn < floor:
				t.Errorf("fan_in = %d, want at least %d", m.FanIn, floor)
			case m.StdlibImports != 0:
				t.Errorf("stdlib_imports = %d, want 0: every import is internal", m.StdlibImports)
			}
		})
	}

	var fanIn, fanOut int
	for path, m := range got {
		if err := m.Validate(); err != nil {
			t.Errorf("%s: Validate: %v", path, err)
		}
		fanIn += m.FanIn
		fanOut += m.InternalImports
	}
	// Every edge counts once on each side, blank, dot and vendored imports
	// included and cgo-generated ones on neither.
	if fanIn == 0 || fanIn != fanOut {
		t.Errorf("sum(fan_in) %d != sum(internal_imports) %d", fanIn, fanOut)
	}
	t.Logf("sum(fan_in) %d, sum(internal_imports) %d", fanIn, fanOut)

	// The single-package path measures source the same way: a cgo package
	// and unsafe, which has no syntax from go/packages, agree on every
	// metric the rest of the library cannot change.
	for _, path := range []string{"unsafe", "net", "runtime/cgo"} {
		one, err := ExtractStdlib(t.Context(), path,
			WithCharsPerToken(cfg.CharsPerToken),
			WithDupMinTokens(cfg.Duplication.MinTokens),
			WithDupIgnoreLiteralOnly(cfg.Duplication.IgnoreLiteralOnly),
			WithDupFoldSigns(cfg.Duplication.FoldSigns))
		if err != nil {
			t.Errorf("ExtractStdlib(%s): %v", path, err)
			continue
		}
		all := got[path]
		if one.Files != all.Files || one.SLOC != all.SLOC || one.FuncCount != all.FuncCount ||
			one.CognitiveTotal != all.CognitiveTotal || one.ExportedSymbols != all.ExportedSymbols ||
			one.TokensEst != all.TokensEst || one.DupBlocks != all.DupBlocks {
			t.Errorf("%s: ExtractStdlib files/sloc/funcs/cognitive/exports/tokens/dups = %d/%d/%d/%d/%d/%d/%d, ExtractStdlibAll %d/%d/%d/%d/%d/%d/%d",
				path, one.Files, one.SLOC, one.FuncCount, one.CognitiveTotal, one.ExportedSymbols, one.TokensEst, one.DupBlocks,
				all.Files, all.SLOC, all.FuncCount, all.CognitiveTotal, all.ExportedSymbols, all.TokensEst, all.DupBlocks)
		}
	}

	http, ok := got["net/http"]
	if !ok {
		t.Fatal("net/http missing")
	}
	est := score.Estimate(http, cfg.Rebuild)
	if tier := score.TierOf(est.AgentPasses, cfg.Rebuild.Tiers); tier != score.TierPartition {
		t.Errorf("net/http tier = %s (agent_passes %.4f, fan_in %d), want %s",
			tier, est.AgentPasses, http.FanIn, score.TierPartition)
	}
	t.Logf("%d packages; fan_in errors %d, fmt %d, io %d; net/http fan_in %d agent_passes %.2f; sum(fan_in) %d",
		len(got), got["errors"].FanIn, got["fmt"].FanIn, got["io"].FanIn, http.FanIn, est.AgentPasses, fanIn)
}

func TestExtractStdlibAllFailures(t *testing.T) {
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	tests := []struct {
		name string
		ctx  context.Context
		opts []Option
		want error
	}{
		{"unknown tokenizer", t.Context(), []Option{WithTokenizer("cl100k")}, metrics.ErrUnknownTokenizer},
		{"cancelled", cancelled, nil, context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := ExtractStdlibAll(tt.ctx, tt.opts...); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}
