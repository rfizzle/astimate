package inspect

import (
	"fmt"
	"math"
	"strings"

	"github.com/pkoukk/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// DefaultCharsPerToken is the byte-to-token ratio SPEC.md 6.1 sets for Go
// source.
const DefaultCharsPerToken = 3.2

// Token counting methods, recorded in TokenCounts.Method. They match the
// values of the --tokenizer flag.
const (
	MethodEst   = "est"
	MethodO200k = "o200k"
)

// TokenCounts holds tokens_est, tokens_est_with_tests and
// tokens_est_generated of one package, and the method that produced them for
// debugging.
type TokenCounts struct {
	TokensEst, TokensEstWithTests, TokensEstGenerated int
	Method                                            string
}

// Counter counts the tokens of a set of source files. The extractor options
// WithTokenizer and WithCharsPerToken choose between NewRatioCounter and
// NewO200kCounter.
type Counter interface {
	// Count returns the total tokens of the files at paths, taking their
	// contents or lengths from src.
	Count(src load.FileSource, paths []string) (int, error)
	// Method names the counting method, for tokens_method.
	Method() string
}

// Tokens computes tokens_est over p's authored non-test files,
// tokens_est_generated over its generated ones (see
// load.Module.AuthoredSyntax), and
// tokens_est_with_tests over the authored files plus the _test.go files of
// p's in-package test variant and external test package. A file listed by
// both variants is counted once. A generated _test.go file stays in
// tokens_est_with_tests: SPEC.md 6.5 excludes generated non-test files only.
// File contents and lengths come from src.
func Tokens(m *load.Module, p *packages.Package, src load.FileSource, c Counter) (TokenCounts, error) {
	generated := m.GeneratedNames(p)
	authored := p.GoFiles
	var genFiles []string
	if len(generated) > 0 {
		authored = make([]string, 0, len(p.GoFiles))
		genFiles = make([]string, 0, len(generated))
		for _, name := range p.GoFiles {
			if generated[name] {
				genFiles = append(genFiles, name)
			} else {
				authored = append(authored, name)
			}
		}
	}
	est, err := c.Count(src, authored)
	if err != nil {
		return TokenCounts{}, fmt.Errorf("counting tokens of %s: %w", p.PkgPath, err)
	}
	gen := 0
	if len(genFiles) > 0 {
		gen, err = c.Count(src, genFiles)
		if err != nil {
			return TokenCounts{}, fmt.Errorf("counting generated tokens of %s: %w", p.PkgPath, err)
		}
	}

	all := make([]string, 0, len(p.GoFiles))
	seen := make(map[string]bool, len(p.GoFiles))
	for _, name := range p.GoFiles {
		seen[name] = true
	}
	all = append(all, authored...)
	for _, tp := range m.TestPackages(p) {
		for _, name := range tp.GoFiles {
			if !strings.HasSuffix(name, "_test.go") || seen[name] {
				continue
			}
			seen[name] = true
			all = append(all, name)
		}
	}
	withTests := est
	if len(all) > len(authored) {
		withTests, err = c.Count(src, all)
		if err != nil {
			return TokenCounts{}, fmt.Errorf("counting tokens of %s with tests: %w", p.PkgPath, err)
		}
	}
	return TokenCounts{TokensEst: est, TokensEstWithTests: withTests, TokensEstGenerated: gen, Method: c.Method()}, nil
}

// ratioCounter estimates tokens as total bytes divided by charsPerToken.
type ratioCounter struct {
	charsPerToken float64
}

// NewRatioCounter returns a counter that divides total file bytes by
// charsPerToken. SPEC.md 6.1 sets the default, DefaultCharsPerToken.
func NewRatioCounter(charsPerToken float64) Counter {
	return ratioCounter{charsPerToken: charsPerToken}
}

// Count sums the lengths src reports for the files at paths, then divides
// once by charsPerToken and truncates (SPEC.md 6.5). It reads no file
// contents; through the extraction's shared cache, files already read cost
// no system call.
func (r ratioCounter) Count(src load.FileSource, paths []string) (int, error) {
	if !(r.charsPerToken > 0) || math.IsInf(r.charsPerToken, 1) {
		return 0, fmt.Errorf("chars per token %v: must be positive and finite", r.charsPerToken)
	}
	total, err := sumFiles(paths, "sizing", src.Length)
	if err != nil {
		return 0, err
	}
	return int(float64(total) / r.charsPerToken), nil
}

// Method returns "est".
func (ratioCounter) Method() string { return MethodEst }

// o200kCounter counts tokens exactly with the o200k_base encoding.
type o200kCounter struct {
	enc *tiktoken.Tiktoken
}

// NewO200kCounter returns a counter using the o200k_base encoding, loaded
// from the BPE file embedded in tiktoken-go-loader, so it makes no network
// calls.
//
// tiktoken-go only accepts a loader through the package-level
// tiktoken.SetBpeLoader, so this constructor sets it, replacing the default
// loader that downloads the BPE file. The library caches the parsed encoding
// process-wide after the first call.
func NewO200kCounter() (Counter, error) {
	tiktoken.SetBpeLoader(tiktokenloader.NewOfflineLoader())
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		return nil, fmt.Errorf("loading o200k_base encoding: %w", err)
	}
	return o200kCounter{enc: enc}, nil
}

// Count reads each file at paths through src and sums its o200k_base token
// count. Special-token text in a file is counted as ordinary text.
func (o o200kCounter) Count(src load.FileSource, paths []string) (int, error) {
	total, err := sumFiles(paths, "reading", func(name string) (int64, error) {
		data, err := src.Read(name)
		if err != nil {
			return 0, err
		}
		return int64(len(o.enc.EncodeOrdinary(string(data)))), nil
	})
	return int(total), err
}

// Method returns "o200k".
func (o200kCounter) Method() string { return MethodO200k }

// sumFiles adds up measure over the files at paths. The first error stops
// it, wrapped with doing and the file's name.
func sumFiles(paths []string, doing string, measure func(name string) (int64, error)) (int64, error) {
	var total int64
	for _, name := range paths {
		n, err := measure(name)
		if err != nil {
			return 0, fmt.Errorf("%s %s: %w", doing, name, err)
		}
		total += n
	}
	return total, nil
}
