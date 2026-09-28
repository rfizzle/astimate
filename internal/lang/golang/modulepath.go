package golang

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
)

// ErrNoModule reports that no go.mod was found at or above a directory.
var ErrNoModule = errors.New("no go.mod found")

// ModulePath returns the module path declared in the go.mod at root.
// Callers use it to fill metrics.ModuleContext.ModulePath before calling
// Extract.
func ModulePath(root string) (string, error) {
	return load.ReadModulePath(root)
}

// FindModuleRoot walks up from dir to the nearest directory containing a
// go.mod and returns its absolute path. It wraps ErrNoModule when none
// exists, so callers can tell a non-module path from an I/O failure.
func FindModuleRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		info, err := os.Stat(filepath.Join(abs, "go.mod"))
		if err == nil && info.Mode().IsRegular() {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", ErrNoModule
		}
		abs = parent
	}
}
