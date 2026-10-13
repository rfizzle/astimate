package resolve

import (
	"maps"
	"os"
	"path/filepath"
	"testing"
)

func TestBinDirs(t *testing.T) {
	tests := []struct {
		name     string
		manifest string // "" writes no package.json
		want     map[string]bool
	}{
		{"no manifest", "", nil},
		{"no bin", `{"name":"x"}`, nil},
		{"string", `{"bin":"./src/cli.ts"}`, map[string]bool{"src": true}},
		{"object", `{"bin":{"a":"bin/a.ts","b":"./tools/b/main.ts"}}`, map[string]bool{"bin": true, "tools/b": true}},
		{"root file", `{"bin":"cli.ts"}`, map[string]bool{".": true}},
		{"outside root", `{"bin":"../x/cli.ts"}`, nil},
		{"other shape", `{"bin":["cli.ts"]}`, nil},
		{"malformed", `{"bin":`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.manifest != "" {
				if err := os.WriteFile(filepath.Join(root, ManifestName), []byte(tt.manifest), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := BinDirs(root); !maps.Equal(got, tt.want) {
				t.Errorf("BinDirs = %v, want %v", got, tt.want)
			}
		})
	}
}
