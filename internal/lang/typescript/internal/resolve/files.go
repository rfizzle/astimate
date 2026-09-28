package resolve

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// ManifestName is the file that marks a module root.
const ManifestName = "package.json"

// testsDir is the directory name whose contents are test files of the
// package that contains it.
const testsDir = "__tests__"

// File is one TypeScript source or test file found under a module root.
type File struct {
	// Abs is the absolute path; Rel the slash path relative to the root.
	Abs, Rel string
	// Pkg is the identifier of the package the file belongs to.
	Pkg  string
	Test bool
}

// FindFiles returns the .ts, .tsx, .mts and .cts files under root that
// belong to the module, sorted by relative path: declaration files (.d.ts,
// .d.mts, .d.cts) are left out, and so are directories named node_modules,
// dist or build, directories whose name starts with a dot, and directories
// below root holding their own package.json, which are modules of their
// own.
func FindFiles(root string) ([]File, error) {
	var out []File
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == root {
				return nil
			}
			if SkipDir(d.Name()) || isFile(filepath.Join(p, ManifestName)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !IsSourceName(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		out = append(out, File{Abs: p, Rel: rel, Pkg: PackageOf(rel), Test: IsTestPath(rel)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b File) int { return strings.Compare(a.Rel, b.Rel) })
	return out, nil
}

// SkipDir reports whether a directory named name is outside every package.
func SkipDir(name string) bool {
	switch name {
	case "node_modules", "dist", "build":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// isFile reports whether p is a regular file.
func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// IsSourceName reports whether a file named name is TypeScript source: it
// ends in .ts, .tsx, .mts or .cts and is not a declaration file (.d.ts,
// .d.mts or .d.cts).
func IsSourceName(name string) bool {
	return IsTypeScriptName(name) && !IsDeclarationName(name)
}

// IsTypeScriptName reports whether a file named name ends in .ts, .tsx,
// .mts or .cts, declaration files included, which end in them too.
func IsTypeScriptName(name string) bool {
	return hasSuffix(name, ".ts", ".tsx", ".mts", ".cts")
}

// IsDeclarationName reports whether a file named name is a declaration
// file: it ends in .d.ts, .d.mts or .d.cts.
func IsDeclarationName(name string) bool {
	return hasSuffix(name, ".d.ts", ".d.mts", ".d.cts")
}

// hasSuffix reports whether s ends in any of suffixes.
func hasSuffix(s string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	return false
}

// IsTestPath reports whether the file at the slash path rel is a test file:
// its name ends in .test or .spec followed by .ts, .tsx, .mts or .cts, or it
// lies under a __tests__ directory.
func IsTestPath(rel string) bool {
	name := path.Base(rel)
	if hasSuffix(name,
		".test.ts", ".spec.ts", ".test.tsx", ".spec.tsx",
		".test.mts", ".spec.mts", ".test.cts", ".spec.cts",
	) {
		return true
	}
	return slices.Contains(strings.Split(path.Dir(rel), "/"), testsDir)
}

// PackageOf returns the identifier of the package the file at the slash
// path rel belongs to: its directory, cut before the first __tests__
// segment, or "." at the root.
func PackageOf(rel string) string {
	return packageDir(path.Dir(rel))
}

// packageDir maps the slash directory dir, relative to the module root, to
// the package directory it belongs to: dir cut before its first __tests__
// segment, "." for the root.
func packageDir(dir string) string {
	segs := strings.Split(dir, "/")
	if i := slices.Index(segs, testsDir); i >= 0 {
		segs = segs[:i]
	}
	if len(segs) == 0 {
		return "."
	}
	return path.Clean(strings.Join(segs, "/"))
}
