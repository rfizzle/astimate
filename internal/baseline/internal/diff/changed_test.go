package diff

import (
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang"
)

// packageDirs returns the sorted, de-duplicated package directories that
// sourceFiles finds among paths with the Go extractor's rules.
func packageDirs(paths []string, prefix string) []string {
	cps := make([]changedPath, 0, len(paths))
	for _, p := range paths {
		cps = append(cps, changedPath{path: p})
	}
	files := sourceFiles(cps, prefix, golang.New())
	dirs := make([]string, 0, len(files.dirs))
	for _, d := range files.dirs {
		dirs = append(dirs, d.pkg)
	}
	return slices.Compact(dirs)
}

func TestPackageDirs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		paths  []string
		prefix string
		want   []string
	}{
		{name: "test file selects its package", paths: []string{"a/a_test.go"}, prefix: ".", want: []string{"a"}},
		{name: "root package is dot", paths: []string{"main.go"}, prefix: ".", want: []string{"."}},
		{name: "non-Go files select nothing", paths: []string{"README.md", "a/data.json", "go.mod", "a/x.go.txt"}, prefix: ".", want: []string{}},
		{name: "duplicates collapse and sort", paths: []string{"b/b.go", "a/a.go", "b/b_test.go", "a/a.go"}, prefix: ".", want: []string{"a", "b"}},
		{name: "testdata ignored at any depth", paths: []string{"testdata/x.go", "a/testdata/y/y.go", "a/a.go"}, prefix: ".", want: []string{"a"}},
		{name: "module below repository root", paths: []string{"sub/a/a.go", "sub/root.go", "other/o.go", "subway/s.go"}, prefix: "sub", want: []string{".", "a"}},
		{name: "prefix is cleaned", paths: []string{"sub/a/a.go"}, prefix: "sub/", want: []string{"a"}},
		{name: "no paths", paths: nil, prefix: ".", want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := packageDirs(tt.paths, tt.prefix)
			if !slices.Equal(got, tt.want) {
				t.Errorf("packageDirs(%q, %q) = %q, want %q", tt.paths, tt.prefix, got, tt.want)
			}
		})
	}
}

func TestParseNameStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		out     string
		want    []changedPath
		wantErr bool
	}{
		{name: "empty", out: "", want: []changedPath{}},
		{
			name: "added, modified, deleted, type changed and unmerged",
			out:  "A\x00a/new.ts\x00M\x00b/b.ts\x00D\x00c/types.d.ts\x00T\x00d/link.ts\x00U\x00e/e.ts\x00",
			want: []changedPath{
				{path: "a/new.ts"},
				{path: "b/b.ts"},
				{path: "c/types.d.ts", deleted: true},
				{path: "d/link.ts"},
				{path: "e/e.ts"},
			},
		},
		{name: "path with a space and a newline", out: "D\x00a b/x\ny.ts\x00", want: []changedPath{{path: "a b/x\ny.ts", deleted: true}}},
		{name: "missing path", out: "M\x00a.ts\x00D\x00", wantErr: true},
		{name: "rename status", out: "R100\x00old.ts\x00", wantErr: true},
		{name: "copy status", out: "C100\x00old.ts\x00", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseNameStatus(tt.out)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseNameStatus(%q) error = %v, want error %v", tt.out, err, tt.wantErr)
			}
			if !tt.wantErr && !slices.Equal(got, tt.want) {
				t.Errorf("parseNameStatus(%q) = %+v, want %+v", tt.out, got, tt.want)
			}
		})
	}
}
