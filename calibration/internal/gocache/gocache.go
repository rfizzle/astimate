// Package gocache keeps the calibration tools' builds out of the user's
// shared Go build cache. A rebuild run, a replayed commit or a collected
// corpus module compiles a whole module and its tests; in the shared
// GOCACHE those entries accumulate across hundreds of runs (242 GB once)
// and are never reused. Pointing GOCACHE and GOTMPDIR into the directory
// that already holds the clone removes them with it.
package gocache

import (
	"fmt"
	"os"
	"path/filepath"
)

// The go command's variables this package sets, and the directories under
// the caller's directory they point at.
const (
	cacheVar = "GOCACHE"
	tmpVar   = "GOTMPDIR"
	cacheDir = "gocache"
	tmpDir   = "gotmp"
)

// setting is one variable and the directory it points at.
type setting struct{ key, dir string }

// settings creates the build cache and temporary directories under dir
// and returns the variables that point at them.
func settings(dir string) ([2]setting, error) {
	s := [2]setting{{cacheVar, filepath.Join(dir, cacheDir)}, {tmpVar, filepath.Join(dir, tmpDir)}}
	for _, v := range s {
		if err := os.MkdirAll(v.dir, 0o755); err != nil {
			return s, fmt.Errorf("creating %s: %w", v.dir, err)
		}
	}
	return s, nil
}

// Env creates a build cache and a temporary directory under dir and
// returns the environment entries that point GOCACHE and GOTMPDIR at them,
// for a go command started with os/exec. Appended after other entries they
// take precedence, since exec uses the last value of a duplicated key.
func Env(dir string) ([]string, error) {
	s, err := settings(dir)
	if err != nil {
		return nil, err
	}
	return []string{s[0].key + "=" + s[0].dir, s[1].key + "=" + s[1].dir}, nil
}

// Setenv creates a build cache and a temporary directory under dir and
// points GOCACHE and GOTMPDIR at them in the process environment, which
// in-process go/packages loads inherit. The returned restore puts the
// previous values back; call it before dir is removed. The process
// environment is shared, so callers must not use Setenv concurrently.
func Setenv(dir string) (restore func(), err error) {
	s, err := settings(dir)
	if err != nil {
		return nil, err
	}
	var prev [len(s)]string
	var had [len(s)]bool
	restore = func() {
		for i, v := range s {
			if had[i] {
				_ = os.Setenv(v.key, prev[i])
			} else {
				_ = os.Unsetenv(v.key)
			}
		}
	}
	for i, v := range s {
		prev[i], had[i] = os.LookupEnv(v.key)
		if err := os.Setenv(v.key, v.dir); err != nil {
			restore()
			return nil, fmt.Errorf("setting %s: %w", v.key, err)
		}
	}
	return restore, nil
}
