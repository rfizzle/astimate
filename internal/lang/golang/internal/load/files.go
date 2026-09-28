package load

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

// FileSource returns the contents or byte length of a source file by its
// absolute name. The metrics that need file bytes (size, duplication and
// tokens) go through one, so a single extraction can share them.
type FileSource interface {
	// Read returns the contents of the file.
	Read(name string) ([]byte, error)
	// Length returns the size of the file in bytes.
	Length(name string) (int64, error)
}

// OSFiles reads files from disk.
type OSFiles struct{}

// Read returns the contents of the file at name.
func (OSFiles) Read(name string) ([]byte, error) {
	return os.ReadFile(name)
}

// Length returns the size of the file at name without opening it.
func (OSFiles) Length(name string) (int64, error) {
	fi, err := os.Stat(name)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// FileCache memoizes the files read through next, so each file is opened at
// most once. It is built per extraction and not kept across extractions: a
// later extraction must see files changed since, and the bytes are held
// only while one package is measured. Callers share the returned bytes and
// must not modify them. A FileCache is not safe for concurrent use.
type FileCache struct {
	next  FileSource
	files map[string][]byte
}

// NewFileCache returns an empty cache over next.
func NewFileCache(next FileSource) *FileCache {
	return &FileCache{next: next, files: make(map[string][]byte)}
}

// Read returns the contents of the file at name, reading it through next on
// first use. A failed read is not cached.
func (c *FileCache) Read(name string) ([]byte, error) {
	if data, ok := c.files[name]; ok {
		return data, nil
	}
	data, err := c.next.Read(name)
	if err != nil {
		return nil, err
	}
	c.files[name] = data
	return data, nil
}

// Length returns the length of the cached contents of the file at name, or
// asks next when the file has not been read, without reading it: the ratio
// token counter sizes _test.go files that nothing else reads, and a stat is
// cheaper than an open.
func (c *FileCache) Length(name string) (int64, error) {
	if data, ok := c.files[name]; ok {
		return int64(len(data)), nil
	}
	return c.next.Length(name)
}

// ReadModulePath returns the module path declared in root/go.mod.
func ReadModulePath(root string) (string, error) {
	name := filepath.Join(root, "go.mod")
	data, err := os.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("reading module path: %w", err)
	}
	p := modfile.ModulePath(data)
	if p == "" {
		return "", fmt.Errorf("reading module path: no module directive in %s", name)
	}
	return p, nil
}

// RelFile returns file relative to dir in slash form, falling back to the
// base name when dir is empty or the file cannot be related to it. An empty
// file stays empty.
func RelFile(dir, file string) string {
	if file == "" {
		return ""
	}
	rel := filepath.Base(file)
	if dir != "" {
		if r, err := filepath.Rel(dir, file); err == nil {
			rel = r
		}
	}
	return filepath.ToSlash(rel)
}
