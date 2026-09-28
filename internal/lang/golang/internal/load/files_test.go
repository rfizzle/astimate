package load

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

// failingFiles is a FileSource whose reads fail until ok is set.
type failingFiles struct {
	ok             bool
	reads, lengths int
}

var errNotYet = errors.New("not yet")

func (f *failingFiles) Read(string) ([]byte, error) {
	f.reads++
	if !f.ok {
		return nil, errNotYet
	}
	return []byte("package x\n"), nil
}

func (f *failingFiles) Length(string) (int64, error) {
	f.lengths++
	return 99, nil
}

func TestFileCache(t *testing.T) {
	next := &failingFiles{}
	c := NewFileCache(next)
	if n, err := c.Length("x.go"); err != nil || n != 99 || next.lengths != 1 {
		t.Errorf("length before read = %d, %v after %d lookups, want next's 99 after 1", n, err, next.lengths)
	}
	if _, err := c.Read("x.go"); !errors.Is(err, errNotYet) {
		t.Fatalf("first read error = %v, want %v", err, errNotYet)
	}
	next.ok = true
	for range 2 {
		data, err := c.Read("x.go")
		if err != nil || string(data) != "package x\n" {
			t.Fatalf("read = %q, %v, want the file", data, err)
		}
	}
	if next.reads != 2 {
		t.Errorf("underlying reads = %d, want 2: one failed, one cached", next.reads)
	}
	if n, err := c.Length("x.go"); err != nil || n != int64(len("package x\n")) || next.lengths != 1 {
		t.Errorf("length after read = %d, %v after %d lookups, want the cached length without a lookup", n, err, next.lengths)
	}
}

func TestOSFiles(t *testing.T) {
	name := filepath.Join(t.TempDir(), "x.go")
	writeFile(t, name, "package x\n")
	var src FileSource = OSFiles{}
	if data, err := src.Read(name); err != nil || string(data) != "package x\n" {
		t.Errorf("Read = %q, %v, want the file", data, err)
	}
	if n, err := src.Length(name); err != nil || n != int64(len("package x\n")) {
		t.Errorf("Length = %d, %v, want %d", n, err, len("package x\n"))
	}
	missing := filepath.Join(t.TempDir(), "missing.go")
	if _, err := src.Read(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Read of a missing file = %v, want fs.ErrNotExist", err)
	}
	if _, err := src.Length(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Length of a missing file = %v, want fs.ErrNotExist", err)
	}
}

func TestRelFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pkg")
	for _, tc := range []struct {
		name, dir, file, want string
	}{
		{"inside", dir, filepath.Join(dir, "sub", "a.go"), "sub/a.go"},
		{"no dir", "", filepath.Join(dir, "a.go"), "a.go"},
		{"empty file", dir, "", ""},
		{"relative file", dir, "a.go", "a.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RelFile(tc.dir, tc.file); got != tc.want {
				t.Errorf("RelFile(%q, %q) = %q, want %q", tc.dir, tc.file, got, tc.want)
			}
		})
	}
}
