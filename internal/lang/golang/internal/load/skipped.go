package load

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// SkippedDir is a module directory with Go files that a load returned no
// package for.
type SkippedDir struct {
	// Dir is the directory relative to the module root, in slash form.
	Dir string
	// ImportPath is the import path the directory's package would have.
	ImportPath string
	// Reason says why the load left it out.
	Reason string
}

// findSkipped walks the module at root, whose module path is modulePath, for
// directories holding Go files that have no package in pkgs, and returns
// them sorted by directory. It skips what "./..." skips: testdata and vendor
// directories, directories whose names start with "_" or ".", and nested
// modules; it ignores files whose names start with "_" or ".", as the go
// command does. go list drops a directory from "./..." without a word when
// build constraints exclude all of its Go files, as they do a package made
// only of cgo files when cgo is disabled. testOnly holds the import paths
// of packages the load listed for their test files alone; see skipReason.
func findSkipped(root, modulePath string, pkgs map[string]*packages.Package, testOnly map[string]bool) ([]SkippedDir, error) {
	var skipped []SkippedDir
	var files []string // Go files of the directory being walked
	dir := ""          // slash path of that directory
	flush := func() {
		if len(files) == 0 {
			return
		}
		path := modulePath
		if dir != "." {
			path += "/" + dir
		}
		if _, ok := pkgs[path]; !ok {
			skipped = append(skipped, SkippedDir{Dir: dir, ImportPath: path, Reason: skipReason(files, testOnly[path])})
		}
		files = files[:0]
	}
	err := filepath.WalkDir(root, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		base := d.Name()
		if !d.IsDir() {
			if strings.HasSuffix(base, ".go") && !ignoredName(base) && d.Type().IsRegular() {
				files = append(files, name)
			}
			return nil
		}
		if name != root {
			if base == "testdata" || base == "vendor" || ignoredName(base) {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(name, "go.mod")); err == nil {
				return filepath.SkipDir
			}
		}
		flush()
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		dir = filepath.ToSlash(rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("finding skipped packages: %w", err)
	}
	flush()
	slices.SortFunc(skipped, func(a, b SkippedDir) int { return strings.Compare(a.Dir, b.Dir) })
	return skipped, nil
}

// ignoredName reports whether the go command ignores a file or directory
// named base: its name starts with "_" or ".".
func ignoredName(base string) bool {
	return strings.HasPrefix(base, "_") || strings.HasPrefix(base, ".")
}

// skipReason says why the directory holding the Go files named by files was
// left out of a load: build constraints exclude them all, and, when one
// imports "C", that cgo is the likely cause. When testOnly is set the load
// kept the directory's test files, so only its non-test files are excluded:
// a package of tests beside a "//go:build ignore" generator. It parses only
// the import clauses.
func skipReason(files []string, testOnly bool) string {
	const reason = "build constraints exclude all Go files"
	if importsC(files) {
		return reason + "; it " + usesDisabledCgo
	}
	if testOnly {
		n := 0
		for _, name := range files {
			if !strings.HasSuffix(name, "_test.go") {
				n++
			}
		}
		noun := " non-test Go files"
		if n == 1 {
			noun = " non-test Go file"
		}
		return "test-only package: build constraints exclude its " + strconv.Itoa(n) + noun
	}
	return reason
}
