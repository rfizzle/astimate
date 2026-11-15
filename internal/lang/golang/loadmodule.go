package golang

import (
	"errors"
	"fmt"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/extract"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// ErrNoPackages is returned when a module root contains no Go packages.
var ErrNoPackages = errors.New("no Go packages in module")

// loadModule loads the module at cfg.Dir, which must be an absolute module
// root, with load.Load, and fails with ErrNoPackages when it has no
// packages. The result is the module context every package of the module
// is measured in.
func loadModule(cfg *packages.Config, fn load.Func) (*extract.Module, error) {
	m, err := load.Load(cfg, fn)
	if err != nil {
		return nil, err
	}
	if len(m.Paths) == 0 {
		return nil, fmt.Errorf("loading %s: %w", cfg.Dir, ErrNoPackages)
	}
	return extract.New(m, cfg.Dir), nil
}
