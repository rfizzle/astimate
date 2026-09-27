package golang

import "os"

// fileSource returns the contents or byte length of a source file by its
// absolute name. The metrics that need file bytes (size, duplication and
// tokens) go through one, so a single extraction can share them.
type fileSource interface {
	// read returns the contents of the file.
	read(name string) ([]byte, error)
	// length returns the size of the file in bytes.
	length(name string) (int64, error)
}

// osFiles reads files from disk.
type osFiles struct{}

// read returns the contents of the file at name.
func (osFiles) read(name string) ([]byte, error) {
	return os.ReadFile(name)
}

// length returns the size of the file at name without opening it.
func (osFiles) length(name string) (int64, error) {
	fi, err := os.Stat(name)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// fileCache memoizes the files read through next, so each file is opened at
// most once. It is built per assemble call and not kept across calls: a
// later extraction must see files changed since, and the bytes are held
// only while one package is measured. Callers share the returned bytes and
// must not modify them. A fileCache is not safe for concurrent use.
type fileCache struct {
	next  fileSource
	files map[string][]byte
}

// newFileCache returns an empty cache over next.
func newFileCache(next fileSource) *fileCache {
	return &fileCache{next: next, files: make(map[string][]byte)}
}

// read returns the contents of the file at name, reading it through next on
// first use. A failed read is not cached.
func (c *fileCache) read(name string) ([]byte, error) {
	if data, ok := c.files[name]; ok {
		return data, nil
	}
	data, err := c.next.read(name)
	if err != nil {
		return nil, err
	}
	c.files[name] = data
	return data, nil
}

// length returns the length of the cached contents of the file at name, or
// asks next when the file has not been read, without reading it: the ratio
// token counter sizes _test.go files that nothing else reads, and a stat is
// cheaper than an open.
func (c *fileCache) length(name string) (int64, error) {
	if data, ok := c.files[name]; ok {
		return int64(len(data)), nil
	}
	return c.next.length(name)
}
