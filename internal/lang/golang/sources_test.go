package golang

import (
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

func TestClassifyFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		rel  string
		want metrics.SourceFile
	}{
		{rel: "main.go", want: metrics.SourceFile{Kind: metrics.PackageSource, Package: "."}},
		{rel: "a/a.go", want: metrics.SourceFile{Kind: metrics.PackageSource, Package: "a"}},
		{rel: "a/a_test.go", want: metrics.SourceFile{Kind: metrics.PackageSource, Package: "a"}},
		{rel: "a/b/c.go", want: metrics.SourceFile{Kind: metrics.PackageSource, Package: "a/b"}},
		{rel: "testdata/x.go"},
		{rel: "a/testdata/y/y.go"},
		{rel: "go.mod"},
		{rel: "go.sum"},
		{rel: "a/x.go.txt"},
		{rel: "README.md"},
	}
	e := New()
	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			t.Parallel()
			if got := e.ClassifyFile(tt.rel); got != tt.want {
				t.Errorf("ClassifyFile(%q) = %+v, want %+v", tt.rel, got, tt.want)
			}
		})
	}
}

func TestIsModuleMarker(t *testing.T) {
	t.Parallel()
	e := New()
	for name, want := range map[string]bool{"go.mod": true, "go.sum": false, "package.json": false} {
		if got := e.IsModuleMarker(name); got != want {
			t.Errorf("IsModuleMarker(%q) = %v, want %v", name, got, want)
		}
	}
}
