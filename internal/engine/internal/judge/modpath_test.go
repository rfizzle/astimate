package judge

import "testing"

func TestRelAndImportPath(t *testing.T) {
	t.Parallel()

	tests := []struct{ modPath, importPath, dir string }{
		{"example.com/m", "example.com/m", "."},
		{"example.com/m", "example.com/m/a", "a"},
		{"example.com/m", "example.com/m/a/b", "a/b"},
		{"", "src/lib", "src/lib"},
		{"", ".", "."},
	}
	for _, tt := range tests {
		t.Run(tt.modPath+"|"+tt.importPath, func(t *testing.T) {
			t.Parallel()
			if got := Rel(tt.modPath, tt.importPath); got != tt.dir {
				t.Errorf("Rel(%q, %q) = %q, want %q", tt.modPath, tt.importPath, got, tt.dir)
			}
			if got := ImportPath(tt.modPath, tt.dir); got != tt.importPath {
				t.Errorf("ImportPath(%q, %q) = %q, want %q", tt.modPath, tt.dir, got, tt.importPath)
			}
		})
	}
}
