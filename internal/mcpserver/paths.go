package mcpserver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// errOutsideWorkDir reports a tool path that resolves outside the working
// directory while Options.AllowAnyPath is off.
var errOutsideWorkDir = errors.New("outside the server's working directory; restart the server with --allow-any-path to allow it")

// workDir returns o.WorkDir, or the process working directory when it is
// empty, as an absolute path with symlinks resolved.
func (o *Options) workDir() (string, error) {
	wd := o.WorkDir
	if wd == "" {
		var err error
		if wd, err = os.Getwd(); err != nil {
			return "", fmt.Errorf("reading working directory: %w", err)
		}
	}
	abs, err := filepath.Abs(wd)
	if err != nil {
		return "", fmt.Errorf("resolving working directory %s: %w", wd, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolving working directory %s: %w", wd, err)
	}
	return resolved, nil
}

// resolvePath resolves a tool's path argument p, relative to the working
// directory unless absolute, to an absolute path with symlinks resolved, so
// a link cannot lead out of the working directory. Unless AllowAnyPath is
// set, a path outside the working directory is refused with an error
// wrapping errOutsideWorkDir. The path must exist. Every tool that takes a
// path resolves it here.
func (o *Options) resolvePath(p string) (string, error) {
	wd, err := o.workDir()
	if err != nil {
		return "", err
	}
	path := p
	if !filepath.IsAbs(path) {
		path = filepath.Join(wd, path)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolving path %q: %w", p, err)
	}
	if o.AllowAnyPath || within(wd, resolved) {
		return resolved, nil
	}
	return "", fmt.Errorf("path %q resolves to %s: %w", p, resolved, errOutsideWorkDir)
}

// within reports whether path is dir or below it; both are absolute and
// clean.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
