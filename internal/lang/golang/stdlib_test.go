package golang

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
	"golang.org/x/tools/go/packages"
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
	l, _, err := loadStdlib(t.Context(), packages.Load)
	if err != nil {
		t.Fatal(err)
	}
	fanOnly, countOnly := oneSidedEdges(l)
	// Every edge both metrics count must count once on each side.
	if fanIn == 0 || fanIn-fanOnly != fanOut-countOnly {
		t.Errorf("sum(fan_in) %d less %d fan_in-only edges != sum(internal_imports) %d less %d internal_imports-only edges",
			fanIn, fanOnly, fanOut, countOnly)
	}
	t.Logf("sum(fan_in) %d (%d edges fan_in alone counts), sum(internal_imports) %d (%d edges internal_imports alone counts)",
		fanIn, fanOnly, fanOut, countOnly)

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

// oneSidedEdges counts, over the packages of l, the import edges fan_in
// counts and internal_imports does not, and the reverse. fan_in takes every
// key of Imports with a standard-library path, which includes blank, dot
// and cgo-generated imports; internal_imports takes the plain imports of
// the source files, which includes the vendored golang.org/x packages under
// their unvendored import path.
func oneSidedEdges(l *loaded) (fanOnly, countOnly int) {
	for _, path := range l.paths {
		p := l.pkgs[path]
		counted := make(map[string]bool)
		for _, f := range sourceSyntax(l, p) {
			for _, spec := range f.Imports {
				ip, err := strconv.Unquote(spec.Path.Value)
				if err != nil || spec.Name != nil && (spec.Name.Name == "_" || spec.Name.Name == ".") {
					continue
				}
				if imp, ok := p.Imports[ip]; ok && classifyImport(l, imp) == importInternal {
					counted[ip] = true
				}
			}
		}
		for ip := range p.Imports {
			if isInternal(l, ip) && !counted[ip] {
				fanOnly++
			}
		}
		for ip := range counted {
			if !isInternal(l, ip) {
				countOnly++
			}
		}
	}
	return fanOnly, countOnly
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
