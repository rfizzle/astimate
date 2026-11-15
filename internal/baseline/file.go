package baseline

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rfizzle/astimate/internal/baseline/internal/fileformat"
	"github.com/rfizzle/astimate/internal/metrics"
)

// DefaultPath is where `astimate baseline write` puts the baseline file,
// relative to the module root.
const DefaultPath = ".astimate/baseline.json"

// DefaultTokenizer is the tokenizer FromFile reports for a file that
// records none, as files written before the field existed do.
const DefaultTokenizer = "est"

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
	// Packages are the metrics keyed by package identifier (the import
	// path for Go); the file also records each one's module-relative
	// directory (PackageDir under ModulePath), the key a check matches by.
	Packages map[string]metrics.RawMetrics
	// Functions are each package's functions keyed by import path, as
	// CollectFunctions returns them; nil records none, so a check against
	// the file leaves changed_func_cognitive_max null.
	Functions map[string][]metrics.FunctionInfo
	// Named are each package's duplicate blocks, untested exports and
	// exports keyed by import path, as CollectNamed returns them; nil
	// records none, so a check against the file cannot tell which of them a
	// change added or made unexported.
	Named map[string]Named
}

// FromFile reads a baseline file written by Write or WriteContents, whose
// layout package fileformat decodes. Its Ref
// and Tokenizer are the ones recorded in the file; a file that records no
// tokenizer reports DefaultTokenizer, and one that records no functions
// serves rows without them (Row.HasFunctions false), likewise details
// (Row.HasNamed false). A file written before metrics.ModuleRowID
// was reserved stores the module row under "module"; FromFile serves that
// row under metrics.ModuleRowID when the file has no row under the new key
// and every v0 field of the row is zero, as on the module row and on no
// package, and MigratedModuleRow then reports true. A row under "module"
// with any v0 field set is the package of that import path.
//
// Rows are keyed by the package_path the file records for each package
// under package_rows. A file written before package_rows existed keys its
// packages by import path only; FromFile then takes each directory from the
// recorded module path (PackageDir) and KeyedByImportPath reports true.
func FromFile(path string) (Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading baseline: %w", err)
	}
	f, err := fileformat.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("decoding baseline %s: %w", path, err)
	}
	if f.Tokenizer == "" {
		f.Tokenizer = DefaultTokenizer
	}
	dir := func(id string) string {
		if d, ok := f.Dirs[id]; ok {
			return d
		}
		return PackageDir(f.ModulePath, id)
	}
	c := collected{pkgs: f.Packages, funcs: f.Functions, named: f.Named, dir: dir}
	return &snapshot{
		ref: f.Ref, tokenizer: f.Tokenizer, rows: c.rows(), migrated: f.MigratedModuleRow, importPathOnly: f.KeyedByImportPath,
	}, nil
}

// KeyedByImportPath reports whether b was read by FromFile from a file that
// keys its packages by import path only, as files written before
// package_rows existed do. b still matches packages by directory, taken
// from the module path the file records; rewriting the file with
// WriteContents records each package's directory.
func KeyedByImportPath(b Baseline) bool {
	s, ok := b.(*snapshot)
	return ok && s.importPathOnly
}

// MigratedModuleRow reports whether b was read by FromFile from a file that
// stores the module row under the key "module", the key used before
// metrics.ModuleRowID was reserved. b serves the row under
// metrics.ModuleRowID either way; rewriting the file with WriteContents
// stores it under that key.
func MigratedModuleRow(b Baseline) bool {
	s, ok := b.(*snapshot)
	return ok && s.migrated
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
	tokenizer := c.Tokenizer
	if tokenizer == "" {
		tokenizer = DefaultTokenizer
	}
	dirs := make(map[string]string, len(c.Packages))
	for id := range c.Packages {
		if id != metrics.ModuleRowID {
			dirs[id] = PackageDir(c.ModulePath, id)
		}
	}
	data, err := fileformat.Encode(fileformat.File{
		Ref:         c.Ref,
		GeneratedAt: time.Now().UTC().Truncate(time.Second),
		ModulePath:  c.ModulePath,
		Tokenizer:   tokenizer,
		Packages:    c.Packages,
		Functions:   c.Functions,
		Named:       c.Named,
		Dirs:        dirs,
	})
	if err != nil {
		return fmt.Errorf("encoding baseline: %w", err)
	}

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
