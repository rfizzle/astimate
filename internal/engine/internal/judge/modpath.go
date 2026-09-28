package judge

import "strings"

// Rel returns the directory of the package with import path importPath
// relative to the root of module modPath, in slash form: "." for the root
// package, matching assess's package_path. With an empty modPath, as for a
// TypeScript module, the identifier already is that directory.
func Rel(modPath, importPath string) string {
	if modPath == "" {
		return importPath
	}
	if importPath == modPath {
		return "."
	}
	return strings.TrimPrefix(importPath, modPath+"/")
}

// ImportPath returns the import path of the package in the module-relative
// slash directory dir of module modPath, or dir itself when modPath is
// empty; the inverse of Rel.
func ImportPath(modPath, dir string) string {
	if modPath == "" {
		return dir
	}
	if dir == "." {
		return modPath
	}
	return modPath + "/" + dir
}
