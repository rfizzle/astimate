package baseline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rfizzle/astimate/internal/metrics"
)

// DefaultPath is where `astimate baseline write` puts the baseline file,
// relative to the module root.
const DefaultPath = ".astimate/baseline.json"

// DefaultTokenizer is the tokenizer FromFile reports for a file that
// records none, as files written before the field existed do.
const DefaultTokenizer = "est"

// fileFormat is the JSON layout of a baseline file (SPEC.md 8.3).
type fileFormat struct {
	Ref         string                        `json:"ref"`
	GeneratedAt time.Time                     `json:"generated_at"`
	ModulePath  string                        `json:"module_path"`
	Tokenizer   string                        `json:"tokenizer,omitempty"`
	Packages    map[string]metrics.RawMetrics `json:"packages"`
	// Functions holds each package's functions for the function-level
	// diff; absent in files written before it existed, which then record
	// none.
	Functions map[string][]fileFunction `json:"functions,omitempty"`
}

// fileFunction is one function in a baseline file: the matching key, the
// fingerprint as 16 hexadecimal digits and the cognitive complexity. File
// and line are not stored; a baseline function is only ever matched, never
// named.
type fileFunction struct {
	Receiver    string `json:"receiver,omitempty"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	Cognitive   int    `json:"cognitive"`
}

// Contents is what a baseline file records besides the time it was
// written.
type Contents struct {
	// Ref is the commit the baseline was taken at; empty outside git.
	Ref string
	// ModulePath is the module's import path.
	ModulePath string
	// Tokenizer names the method that counted tokens_est; empty records
	// DefaultTokenizer.
	Tokenizer string
	// Packages are the metrics keyed by import path.
	Packages map[string]metrics.RawMetrics
	// Functions are each package's functions keyed by import path, as
	// CollectFunctions returns them; nil records none, so a check against
	// the file leaves changed_func_cognitive_max null.
	Functions map[string][]metrics.FunctionInfo
}

// FromFile reads a baseline file written by Write or WriteContents. Its Ref
// and Tokenizer are the ones recorded in the file; a file that records no
// tokenizer reports DefaultTokenizer, and one that records no functions
// reports none from Functions.
func FromFile(path string) (Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading baseline: %w", err)
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("decoding baseline %s: %w", path, err)
	}
	if f.Packages == nil {
		f.Packages = map[string]metrics.RawMetrics{}
	}
	if f.Tokenizer == "" {
		f.Tokenizer = DefaultTokenizer
	}
	funcs, err := decodeFunctions(f.Functions)
	if err != nil {
		return nil, fmt.Errorf("decoding baseline %s: %w", path, err)
	}
	return &snapshot{ref: f.Ref, tokenizer: f.Tokenizer, pkgs: f.Packages, funcs: funcs}, nil
}

// decodeFunctions converts the file's function records, parsing each
// fingerprint; nil stays nil.
func decodeFunctions(in map[string][]fileFunction) (map[string][]metrics.FunctionInfo, error) {
	if in == nil {
		return nil, nil
	}
	out := make(map[string][]metrics.FunctionInfo, len(in))
	for pkg, ffs := range in {
		fns := make([]metrics.FunctionInfo, len(ffs))
		for i, ff := range ffs {
			fp, err := strconv.ParseUint(ff.Fingerprint, 16, 64)
			if err != nil {
				return nil, fmt.Errorf("function %s of %s: fingerprint %q: %w", ff.Name, pkg, ff.Fingerprint, err)
			}
			fns[i] = metrics.FunctionInfo{Receiver: ff.Receiver, Name: ff.Name, Fingerprint: fp, Cognitive: ff.Cognitive}
		}
		out[pkg] = fns
	}
	return out, nil
}

// encodeFunctions converts functions to their file records; nil stays nil.
func encodeFunctions(in map[string][]metrics.FunctionInfo) map[string][]fileFunction {
	if in == nil {
		return nil
	}
	out := make(map[string][]fileFunction, len(in))
	for pkg, fns := range in {
		ffs := make([]fileFunction, len(fns))
		for i := range fns {
			fp := strconv.FormatUint(fns[i].Fingerprint, 16)
			fp = strings.Repeat("0", 16-len(fp)) + fp
			ffs[i] = fileFunction{Receiver: fns[i].Receiver, Name: fns[i].Name, Fingerprint: fp, Cognitive: fns[i].Cognitive}
		}
		out[pkg] = ffs
	}
	return out
}

// Write stores pkgs, keyed by import path, as a baseline file at path
// recording no functions; it is WriteContents with only Ref, ModulePath,
// Tokenizer and Packages set.
func Write(path, ref, modulePath, tokenizer string, pkgs map[string]metrics.RawMetrics) error {
	return WriteContents(path, Contents{Ref: ref, ModulePath: modulePath, Tokenizer: tokenizer, Packages: pkgs})
}

// WriteContents stores c as a baseline file at path with mode 0644,
// recording the current time. It writes a temporary file in the same
// directory and renames it into place, so a reader never sees a partial
// file. The directory must exist.
func WriteContents(path string, c Contents) (err error) {
	pkgs := c.Packages
	if pkgs == nil {
		pkgs = map[string]metrics.RawMetrics{}
	}
	tokenizer := c.Tokenizer
	if tokenizer == "" {
		tokenizer = DefaultTokenizer
	}
	data, err := json.MarshalIndent(fileFormat{
		Ref:         c.Ref,
		GeneratedAt: time.Now().UTC().Truncate(time.Second),
		ModulePath:  c.ModulePath,
		Tokenizer:   tokenizer,
		Packages:    pkgs,
		Functions:   encodeFunctions(c.Functions),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding baseline: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("writing baseline %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("writing baseline %s: %w", path, err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		return fmt.Errorf("writing baseline %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing baseline %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("writing baseline %s: %w", path, err)
	}
	return nil
}
