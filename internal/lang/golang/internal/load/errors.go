package load

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// usesDisabledCgo completes a message about a package that build constraints
// left without Go files because they import "C".
const usesDisabledCgo = "uses cgo, which is disabled (CGO_ENABLED=0, the default when no C compiler is on PATH)"

// packageError returns the load error of the module package p, which has
// errors. When the failure traces to a build cache go list could not use, it
// returns that go list error, naming the cause, even if a type error follows
// it. When it traces to cgo that could not run, it names that cause ahead of
// the error it produced, which on its own ("could not import C",
// "undefined: dep.F") does not.
func packageError(p *packages.Package) error {
	for _, e := range p.Errors {
		if e.Kind == packages.ListError && fromCache(e.Msg) {
			return fmt.Errorf("loading %s: %w", p.PkgPath, cacheCause(e))
		}
	}
	err := firstError(p.Errors)
	if cause := cgoCause(p); cause != "" {
		return fmt.Errorf("loading %s: %s: %w", p.PkgPath, cause, err)
	}
	return fmt.Errorf("loading %s: %w", p.PkgPath, err)
}

// cgoCause describes why p failed to load when the cause is cgo, and
// returns "" otherwise. It looks at p, then its direct imports in import
// path order: a package whose import "C" failed could not be preprocessed
// because cgo's C compiler did not run, and a dependency whose only files
// import "C" was excluded because cgo is disabled. A dependency whose export
// data cannot be built does not fail the load by itself, because go/packages
// then type-checks it from source; only a module package that uses what cgo
// left out fails, so only direct imports need a look.
func cgoCause(p *packages.Package) string {
	if importsCFailed(p) {
		return "cgo package " + p.PkgPath + " needs a C compiler (" + ccSetting() + ")"
	}
	imports := make([]string, 0, len(p.Imports))
	for path := range p.Imports {
		imports = append(imports, path)
	}
	slices.Sort(imports)
	for _, path := range imports {
		imp := p.Imports[path]
		switch {
		case importsCFailed(imp):
			return "cgo dependency " + path + " needs a C compiler (" + ccSetting() + ")"
		case cgoExcluded(imp):
			return "dependency " + path + " " + usesDisabledCgo
		}
	}
	return ""
}

// importsCFailed reports whether type-checking p failed to import "C", which
// happens when go list could not run cgo on p's files.
func importsCFailed(p *packages.Package) bool {
	return slices.ContainsFunc(p.Errors, func(e packages.Error) bool {
		return strings.Contains(e.Msg, "could not import C (")
	})
}

// cgoExcluded reports whether build constraints left p with no Go files and
// one of the files they excluded imports "C". It runs only on the error
// path.
func cgoExcluded(p *packages.Package) bool {
	return len(p.GoFiles) == 0 && importsC(p.IgnoredFiles)
}

// importsC reports whether one of the Go files named by files imports "C".
// It parses only the import clauses and skips a file that fails to parse.
func importsC(files []string) bool {
	fset := token.NewFileSet()
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			continue
		}
		for _, spec := range f.Imports {
			if spec.Path.Value == `"C"` {
				return true
			}
		}
	}
	return false
}

// ccSetting describes the C compiler cgo runs: the CC environment variable,
// or the go command's default when it is unset.
func ccSetting() string {
	if cc := os.Getenv("CC"); cc != "" {
		return "CC=" + cc
	}
	return "CC unset, go env CC names the default"
}

// cacheCause wraps err, a failure of go list itself, with its cause when it
// came from the build cache, and returns err unchanged otherwise. Loading
// without NeedDeps runs go list -export, which writes export data to
// GOCACHE, so a read-only cache (or GOCACHE=off) fails every load.
func cacheCause(err error) error {
	if !fromCache(err.Error()) {
		return err
	}
	dir := goCacheDir()
	if dir == "" {
		dir = "unknown"
	}
	return fmt.Errorf("the Go build cache must be writable (GOCACHE=%s): %w", dir, err)
}

// fromCache reports whether msg, a go list error, is about the build cache:
// it mentions the build cache or a path inside it.
func fromCache(msg string) bool {
	if strings.Contains(msg, "build cache") {
		return true
	}
	dir := goCacheDir()
	return dir != "" && strings.Contains(msg, dir)
}

// goCacheDir returns the build cache directory: GOCACHE, or the go
// command's default under the user cache directory when it is unset. It
// returns "" when neither is known.
func goCacheDir() string {
	if dir := os.Getenv("GOCACHE"); dir != "" {
		return dir
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "go-build")
}
