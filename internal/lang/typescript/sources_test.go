package typescript

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
		{rel: "a/a.ts", want: metrics.SourceFile{Kind: metrics.PackageSource, Package: "a"}},
		{rel: "index.tsx", want: metrics.SourceFile{Kind: metrics.PackageSource, Package: "."}},
		{rel: "a/b/esm.mts", want: metrics.SourceFile{Kind: metrics.PackageSource, Package: "a/b"}},
		{rel: "a/common.cts", want: metrics.SourceFile{Kind: metrics.PackageSource, Package: "a"}},
		{rel: "a/a.test.ts", want: metrics.SourceFile{Kind: metrics.MemberSource, Package: "a"}},
		{rel: "a/a.spec.tsx", want: metrics.SourceFile{Kind: metrics.MemberSource, Package: "a"}},
		{rel: "a/__tests__/deep/helper.ts", want: metrics.SourceFile{Kind: metrics.MemberSource, Package: "a"}},
		{rel: "__tests__/x.test.ts", want: metrics.SourceFile{Kind: metrics.MemberSource, Package: "."}},
		{rel: "a/types.d.ts", want: metrics.SourceFile{Kind: metrics.MemberSource, Package: "a", Contract: true}},
		{rel: "a/types.d.mts", want: metrics.SourceFile{Kind: metrics.MemberSource, Package: "a", Contract: true}},
		{rel: "a/types.d.cts", want: metrics.SourceFile{Kind: metrics.MemberSource, Package: "a", Contract: true}},
		{rel: "a/__tests__/env.d.ts", want: metrics.SourceFile{Kind: metrics.MemberSource, Package: "a", Contract: true}},
		{rel: "package.json", want: metrics.SourceFile{Kind: metrics.ModuleSource}},
		{rel: "tsconfig.json", want: metrics.SourceFile{Kind: metrics.ModuleSource}},
		{rel: "tsconfig.build.json", want: metrics.SourceFile{Kind: metrics.ModuleSource}},
		{rel: "config/tsconfig.base.json", want: metrics.SourceFile{Kind: metrics.ModuleSource}},
		{rel: "tools/package.json"},
		{rel: "a/a.js"},
		{rel: "README.md"},
		{rel: ".eslintrc.json"},
		{rel: "node_modules/dep/index.ts"},
		{rel: "node_modules/dep/tsconfig.json"},
		{rel: "dist/a.ts"},
		{rel: "a/build/b.ts"},
		{rel: ".github/x.ts"},
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
	for name, want := range map[string]bool{"package.json": true, "tsconfig.json": false, "go.mod": false} {
		if got := e.IsModuleMarker(name); got != want {
			t.Errorf("IsModuleMarker(%q) = %v, want %v", name, got, want)
		}
	}
}
