package golang

import (
	"fmt"
	"math"
	"strings"

	"github.com/pkoukk/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"
	"golang.org/x/tools/go/packages"
)

// defaultCharsPerToken is the byte-to-token ratio SPEC.md 6.1 sets for Go
// source.
const defaultCharsPerToken = 3.2

// Token counting methods, recorded in tokenCounts.method. They match the
// values of the --tokenizer flag.
const (
	methodEst   = "est"
	methodO200k = "o200k"
)

// tokenCounts holds tokens_est and tokens_est_with_tests of one package, and
// the method that produced them for debugging.
type tokenCounts struct {
	tokensEst, tokensEstWithTests int
	method                        string
}

// tokenCounter counts the tokens of a set of source files. The extractor
// options WithTokenizer and WithCharsPerToken choose between newRatioCounter
// and newO200kCounter.
type tokenCounter interface {
	// Count returns the total tokens of the files at paths, taking their
	// contents or lengths from src.
	Count(src fileSource, paths []string) (int, error)
	// Method names the counting method, for tokens_method.
	Method() string
}

// tokens computes tokens_est over p's non-test files and
// tokens_est_with_tests over those plus the _test.go files of p's in-package
// test variant and external test package. A file listed by both variants is
// counted once. File contents and lengths come from src.
func tokens(l *loaded, p *packages.Package, src fileSource, c tokenCounter) (tokenCounts, error) {
	est, err := c.Count(src, p.GoFiles)
	if err != nil {
		return tokenCounts{}, fmt.Errorf("counting tokens of %s: %w", p.PkgPath, err)
	}

	all := make([]string, 0, len(p.GoFiles))
	seen := make(map[string]bool, len(p.GoFiles))
	for _, name := range p.GoFiles {
		seen[name] = true
		all = append(all, name)
	}
	for _, tp := range testPackagesFor(l, p) {
		for _, name := range tp.GoFiles {
			if !strings.HasSuffix(name, "_test.go") || seen[name] {
				continue
			}
			seen[name] = true
			all = append(all, name)
		}
	}
	withTests := est
	if len(all) > len(p.GoFiles) {
		withTests, err = c.Count(src, all)
		if err != nil {
			return tokenCounts{}, fmt.Errorf("counting tokens of %s with tests: %w", p.PkgPath, err)
		}
	}
	return tokenCounts{tokensEst: est, tokensEstWithTests: withTests, method: c.Method()}, nil
}

// ratioCounter estimates tokens as total bytes divided by charsPerToken.
type ratioCounter struct {
	charsPerToken float64
}

// newRatioCounter returns a counter that divides total file bytes by
// charsPerToken. SPEC.md 6.1 sets the default, defaultCharsPerToken.
func newRatioCounter(charsPerToken float64) tokenCounter {
	return ratioCounter{charsPerToken: charsPerToken}
}

// Count sums the lengths src reports for the files at paths, then divides
// once by charsPerToken and truncates (SPEC.md 6.5). It reads no file
// contents; through the extraction's shared cache, files already read cost
// no system call.
func (r ratioCounter) Count(src fileSource, paths []string) (int, error) {
	if !(r.charsPerToken > 0) || math.IsInf(r.charsPerToken, 1) {
		return 0, fmt.Errorf("chars per token %v: must be positive and finite", r.charsPerToken)
	}
	var total int64
	for _, name := range paths {
		n, err := src.length(name)
		if err != nil {
			return 0, fmt.Errorf("sizing %s: %w", name, err)
		}
		total += n
	}
	return int(float64(total) / r.charsPerToken), nil
}

// Method returns "est".
func (ratioCounter) Method() string { return methodEst }

// o200kCounter counts tokens exactly with the o200k_base encoding.
type o200kCounter struct {
	enc *tiktoken.Tiktoken
}

// newO200kCounter returns a counter using the o200k_base encoding, loaded
// from the BPE file embedded in tiktoken-go-loader, so it makes no network
// calls.
//
// tiktoken-go only accepts a loader through the package-level
// tiktoken.SetBpeLoader, so this constructor sets it, replacing the default
// loader that downloads the BPE file. The library caches the parsed encoding
// process-wide after the first call.
func newO200kCounter() (tokenCounter, error) {
	tiktoken.SetBpeLoader(tiktokenloader.NewOfflineLoader())
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	if err != nil {
		return nil, fmt.Errorf("loading o200k_base encoding: %w", err)
	}
	return o200kCounter{enc: enc}, nil
}

// Count reads each file at paths through src and sums its o200k_base token
// count. Special-token text in a file is counted as ordinary text.
func (o o200kCounter) Count(src fileSource, paths []string) (int, error) {
	total := 0
	for _, name := range paths {
		data, err := src.read(name)
		if err != nil {
			return 0, fmt.Errorf("reading %s: %w", name, err)
		}
		total += len(o.enc.EncodeOrdinary(string(data)))
	}
	return total, nil
}

// Method returns "o200k".
func (o200kCounter) Method() string { return methodO200k }
