package typescript

import (
	"path"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
)

// ClassifyFile implements metrics.SourceClassifier with the counting rules
// of SPEC.md 13.1. A .ts, .tsx, .mts or .cts file belongs to the package
// packageOf names: a source file makes it a package, while a test file or
// a declaration file only belongs to it (a declaration file counts nowhere
// but can change where imports resolve). The root package.json and any
// tsconfig*.json, which the root tsconfig.json may extend, can change the
// resolution of every package's imports. Files under node_modules, dist,
// build or a dot-prefixed directory, and everything else, move nothing.
func (e *Extractor) ClassifyFile(rel string) metrics.SourceFile {
	rel = path.Clean(rel)
	dir, name := path.Split(rel)
	dir = path.Clean(dir)
	if dir != "." {
		for seg := range strings.SplitSeq(dir, "/") {
			if skipDir(seg) {
				return metrics.SourceFile{}
			}
		}
	}
	switch {
	case name == manifestName && dir == ".",
		strings.HasPrefix(name, "tsconfig") && strings.HasSuffix(name, ".json"):
		return metrics.SourceFile{Kind: metrics.ModuleSource}
	case isSourceName(name) && !isTestPath(rel):
		return metrics.SourceFile{Kind: metrics.PackageSource, Package: packageOf(rel)}
	case isTypeScriptName(name):
		return metrics.SourceFile{Kind: metrics.MemberSource, Package: packageOf(rel)}
	}
	return metrics.SourceFile{}
}

// IsModuleMarker implements metrics.SourceClassifier: a package.json file
// starts a module.
func (e *Extractor) IsModuleMarker(name string) bool {
	return name == manifestName
}

// isTypeScriptName reports whether a file named name ends in .ts, .tsx,
// .mts or .cts, declaration files included.
func isTypeScriptName(name string) bool {
	for _, ext := range []string{".ts", ".tsx", ".mts", ".cts"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}
