// Package fileformat is the JSON layout of a baseline file (SPEC.md 8.3):
// encoding what a baseline records into it and decoding it back, the
// layouts of files written before a field existed included. It neither
// reads nor writes files and knows nothing of git; package baseline does
// both and serves the decoded contents as a Baseline.
package fileformat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rfizzle/astimate/internal/metrics"
)

// legacyModuleKey is the key files written before metrics.ModuleRowID was
// reserved stored the module row under. It is also the import path of the
// root package of a module named "module", so Decode reads it as the
// module row only when the row it keys has the module row's shape.
const legacyModuleKey = "module"

// Named is the part of a package's metrics.Details a baseline records.
type Named struct {
	// DupBlocks are the anchors of the package's duplicate blocks
	// (metrics.DupBlock), both of each block's, sorted and distinct.
	DupBlocks []uint64
	// UntestedExports names the package's untested exports, sorted.
	UntestedExports []string
	// Exports are the package's exported declarations
	// (metrics.Details.Exports) without positions, sorted by receiver,
	// then name, then kind; nil in a baseline file written before they
	// were recorded, against which no export reads as made unexported.
	Exports []metrics.Symbol
}

// File is what a baseline file holds, keyed by package identifier (the
// import path for Go), as Encode writes it and Decode reads it.
type File struct {
	// Ref is the commit the baseline was taken at; empty outside git.
	Ref string
	// GeneratedAt is when the file was written.
	GeneratedAt time.Time
	// ModulePath is the module's import path.
	ModulePath string
	// Tokenizer names the method that counted tokens_est; Decode leaves it
	// empty for a file that records none.
	Tokenizer string
	// Packages are the metrics keyed by package identifier, the module row
	// under metrics.ModuleRowID; never nil from Decode.
	Packages map[string]metrics.RawMetrics
	// Functions are each package's functions keyed like Packages; nil
	// records none. The file stores no File or Line.
	Functions map[string][]metrics.FunctionInfo
	// Named are each package's details keyed like Packages; nil when no
	// package records any.
	Named map[string]Named
	// Dirs are each package's module-relative directory in slash form,
	// keyed like Packages; the module row has none. Encode records one for
	// each package of Packages. From Decode, nil for a file written before
	// directories were recorded, and a package recorded without one has no
	// entry.
	Dirs map[string]string
	// MigratedModuleRow reports that Decode moved the module row from
	// legacyModuleKey to metrics.ModuleRowID (migrateModuleRow); Encode
	// ignores it.
	MigratedModuleRow bool
	// KeyedByImportPath reports that Decode read a file that keys its
	// packages by import path only, as files written before package_rows
	// existed do; Encode ignores it.
	KeyedByImportPath bool
}

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
	// PackageRows holds each package's own record, keyed like Packages;
	// the module row has none. Absent (nil) in files written before it
	// existed, which key packages by import path only.
	PackageRows map[string]fileRow `json:"package_rows"`
}

// fileRow is a package's own record in a baseline file, beside its metrics
// and functions: the per-package fields of baseline.Row that have no
// top-level map of their own. A new per-package record is a field here and
// in baseline.Row.
type fileRow struct {
	// PackagePath is the package's directory relative to the module root,
	// in slash form, the key the baseline matches packages by.
	PackagePath string `json:"package_path"`
	// DupBlocks are the anchors of the package's duplicate blocks
	// (Named.DupBlocks), 16 hexadecimal digits each, and UntestedExports
	// the names of its untested exports. Both are absent from files
	// written before they existed, whose rows record no details
	// (baseline.Row.HasNamed false); a pointer keeps a recorded empty list
	// apart from an absent one.
	DupBlocks       *[]string `json:"dup_blocks,omitempty"`
	UntestedExports *[]string `json:"untested_exports,omitempty"`
	// Exports are the package's exported declarations (Named.Exports),
	// absent from files written before they were recorded.
	Exports *[]fileSymbol `json:"exports,omitempty"`
}

// fileSymbol is one exported declaration in a baseline file.
type fileSymbol struct {
	Kind     string `json:"kind"`
	Receiver string `json:"receiver,omitempty"`
	Name     string `json:"name"`
}

// fileFunction is one function in a baseline file: the matching key, the
// fingerprint and an exported function's signature hash as 16 hexadecimal
// digits and the cognitive and cyclomatic complexity; the signature and
// cyclomatic complexity are omitted by files written before they existed,
// and the signature for an unexported function. File and line are not
// stored; a baseline function is only ever matched, never named.
type fileFunction struct {
	Receiver    string `json:"receiver,omitempty"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	Signature   string `json:"signature,omitempty"`
	Cognitive   int    `json:"cognitive"`
	Cyclomatic  int    `json:"cyclomatic,omitempty"`
}

// Decode reads the baseline file data. A file that records no functions
// has nil Functions, likewise details and Named. A file written before
// metrics.ModuleRowID was reserved stores the module row under "module";
// Decode serves that row under metrics.ModuleRowID when the file has no
// row under the new key and every v0 field of the row is zero, as on the
// module row and on no package, and sets MigratedModuleRow. A row under
// "module" with any v0 field set is the package of that import path. A
// file written before package_rows existed has nil Dirs and, when it holds
// a package, KeyedByImportPath set.
func Decode(data []byte) (File, error) {
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, err
	}
	if f.Packages == nil {
		f.Packages = map[string]metrics.RawMetrics{}
	}
	funcs, err := decodeFunctions(f.Functions)
	if err != nil {
		return File{}, err
	}
	out := File{
		Ref: f.Ref, GeneratedAt: f.GeneratedAt, ModulePath: f.ModulePath, Tokenizer: f.Tokenizer,
		Packages: f.Packages, Functions: funcs, MigratedModuleRow: migrateModuleRow(f.Packages),
	}
	if f.PackageRows == nil {
		for id := range f.Packages {
			if id != metrics.ModuleRowID {
				out.KeyedByImportPath = true
				break
			}
		}
	} else {
		out.Dirs = make(map[string]string, len(f.PackageRows))
		for id, r := range f.PackageRows {
			if r.PackagePath != "" {
				out.Dirs[id] = r.PackagePath
			}
		}
	}
	if out.Named, err = decodeNamed(f.PackageRows); err != nil {
		return File{}, err
	}
	return out, nil
}

// Encode renders f as a baseline file, indented and with the module row's
// key stored as written. A nil Packages is written as an empty map.
func Encode(f File) ([]byte, error) {
	pkgs := f.Packages
	if pkgs == nil {
		pkgs = map[string]metrics.RawMetrics{}
	}
	// Without SetEscapeHTML(false) metrics.ModuleRowID would be stored as
	// "<module>".
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err := enc.Encode(fileFormat{
		Ref:         f.Ref,
		GeneratedAt: f.GeneratedAt,
		ModulePath:  f.ModulePath,
		Tokenizer:   f.Tokenizer,
		Packages:    pkgs,
		Functions:   encodeFunctions(f.Functions),
		PackageRows: packageRows(pkgs, f.Dirs, f.Named),
	})
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// migrateModuleRow moves the module row of a file written before
// metrics.ModuleRowID was reserved from legacyModuleKey to
// metrics.ModuleRowID in pkgs, and reports whether it did. It leaves pkgs
// alone when it already has a row under metrics.ModuleRowID, has none
// under legacyModuleKey, or has a package there: a row with a v0 field
// set, which the module row never has.
func migrateModuleRow(pkgs map[string]metrics.RawMetrics) bool {
	if _, ok := pkgs[metrics.ModuleRowID]; ok {
		return false
	}
	m, ok := pkgs[legacyModuleKey]
	if !ok || !zeroV0(&m) {
		return false
	}
	delete(pkgs, legacyModuleKey)
	pkgs[metrics.ModuleRowID] = m
	return true
}

// zeroV0 reports whether every v0 field of m is zero: the v0 fields are the
// ones a zero RawMetrics reports a value for.
func zeroV0(m *metrics.RawMetrics) bool {
	var zero metrics.RawMetrics
	for _, name := range metrics.MetricNames() {
		if _, v0 := zero.Value(name); !v0 {
			continue
		}
		if v, _ := m.Value(name); v != 0 {
			return false
		}
	}
	return true
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
			fp, err := parseHex16(ff.Fingerprint)
			if err != nil {
				return nil, fmt.Errorf("function %s of %s: fingerprint %q: %w", ff.Name, pkg, ff.Fingerprint, err)
			}
			var sig uint64
			if ff.Signature != "" {
				if sig, err = parseHex16(ff.Signature); err != nil {
					return nil, fmt.Errorf("function %s of %s: signature %q: %w", ff.Name, pkg, ff.Signature, err)
				}
			}
			fns[i] = metrics.FunctionInfo{Receiver: ff.Receiver, Name: ff.Name, Fingerprint: fp, Signature: sig,
				Cognitive: ff.Cognitive, Cyclomatic: ff.Cyclomatic}
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
			ffs[i] = fileFunction{Receiver: fns[i].Receiver, Name: fns[i].Name, Fingerprint: hex16(fns[i].Fingerprint), Cognitive: fns[i].Cognitive, Cyclomatic: fns[i].Cyclomatic}
			if fns[i].Signature != 0 {
				ffs[i].Signature = hex16(fns[i].Signature)
			}
		}
		out[pkg] = ffs
	}
	return out
}

// hex16 renders fp as 16 hexadecimal digits.
func hex16(fp uint64) string {
	s := strconv.FormatUint(fp, 16)
	return strings.Repeat("0", 16-len(s)) + s
}

// parseHex16 parses a fingerprint hex16 rendered.
func parseHex16(s string) (uint64, error) {
	return strconv.ParseUint(s, 16, 64)
}

// parseAnchors parses each of hex, an anchor hex16 rendered.
func parseAnchors(hex []string) ([]uint64, error) {
	out := make([]uint64, len(hex))
	for i, s := range hex {
		a, err := parseHex16(s)
		if err != nil {
			return nil, fmt.Errorf("anchor %q: %w", s, err)
		}
		out[i] = a
	}
	return out, nil
}

// decodeNamed returns the details the file's per-package records hold,
// keyed like them, or nil when none records any.
func decodeNamed(rows map[string]fileRow) (map[string]Named, error) {
	var out map[string]Named
	for id, r := range rows {
		if r.DupBlocks == nil && r.UntestedExports == nil && r.Exports == nil {
			continue
		}
		var n Named
		if r.DupBlocks != nil {
			var err error
			if n.DupBlocks, err = parseAnchors(*r.DupBlocks); err != nil {
				return nil, fmt.Errorf("duplicate block of %s: %w", id, err)
			}
		}
		if r.UntestedExports != nil {
			n.UntestedExports = *r.UntestedExports
		}
		if r.Exports != nil && len(*r.Exports) > 0 {
			n.Exports = make([]metrics.Symbol, len(*r.Exports))
			for i, e := range *r.Exports {
				n.Exports[i] = metrics.Symbol{Kind: e.Kind, Receiver: e.Receiver, Name: e.Name}
			}
		}
		if out == nil {
			out = make(map[string]Named, len(rows))
		}
		out[id] = n
	}
	return out, nil
}

// packageRows returns the file's per-package records of pkgs, keyed like
// pkgs, each with its directory from dirs and its details from named when
// it has an entry there; the module row has none. Never nil, so the file
// says it records them.
func packageRows(pkgs map[string]metrics.RawMetrics, dirs map[string]string, named map[string]Named) map[string]fileRow {
	rows := make(map[string]fileRow, len(pkgs))
	for id := range pkgs {
		if id == metrics.ModuleRowID {
			continue
		}
		r := fileRow{PackagePath: dirs[id]}
		if n, ok := named[id]; ok {
			blocks := make([]string, len(n.DupBlocks))
			for i, fp := range n.DupBlocks {
				blocks[i] = hex16(fp)
			}
			untested := append([]string{}, n.UntestedExports...)
			exports := make([]fileSymbol, len(n.Exports))
			for i, e := range n.Exports {
				exports[i] = fileSymbol{Kind: e.Kind, Receiver: e.Receiver, Name: e.Name}
			}
			r.DupBlocks, r.UntestedExports, r.Exports = &blocks, &untested, &exports
		}
		rows[id] = r
	}
	return rows
}
