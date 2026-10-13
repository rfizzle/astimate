package load

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// embedModule writes a module whose package web embeds "all:build" and
// returns its root. When withBuild is set the directory exists. extra is
// appended to web's source.
func embedModule(t *testing.T, withBuild bool, extra string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/emb\n\ngo 1.22\n")
	web := filepath.Join(root, "web")
	if err := os.MkdirAll(web, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(web, "web.go"),
		"package web\n\nimport \"embed\"\n\n//go:embed all:build\nvar FS embed.FS\n"+extra)
	writeFile(t, filepath.Join(web, "web_test.go"), "package web\n")
	if withBuild {
		if err := os.MkdirAll(filepath.Join(web, "build"), 0o700); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(web, "build", "index.html"), "<html>")
	}
	return root
}

func TestLoadEmbedWithoutFiles(t *testing.T) {
	missing, err := Load(&packages.Config{Dir: embedModule(t, false, "")}, packages.Load)
	if err != nil {
		t.Fatalf("Load without the embedded directory: %v", err)
	}
	present := loadRoot(t, embedModule(t, true, ""))

	want := []EmbedWarning{{Package: "example.com/emb/web", Pattern: "all:build"}}
	if !slices.Equal(missing.Warnings, want) {
		t.Errorf("warnings = %+v, want %+v", missing.Warnings, want)
	}
	if len(present.Warnings) != 0 {
		t.Errorf("warnings with the directory present = %+v, want none", present.Warnings)
	}
	if !slices.Equal(missing.Paths, present.Paths) {
		t.Errorf("paths = %v, want %v", missing.Paths, present.Paths)
	}
	got := missing.Pkgs["example.com/emb/web"]
	if got == nil || len(got.Syntax) != 1 || got.Types == nil {
		t.Errorf("web not loaded with syntax and types: %+v", got)
	}
	if _, ok := missing.Tests["example.com/emb/web"]; !ok {
		t.Error("web's test variant was dropped")
	}
}

func TestLoadEmbedWithOtherError(t *testing.T) {
	for _, tc := range []struct {
		name      string
		withBuild bool
	}{
		{"embed missing", false},
		{"embed present", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := embedModule(t, tc.withBuild, "\nvar _ = undefinedName\n")
			_, err := Load(&packages.Config{Dir: root}, packages.Load)
			if err == nil {
				t.Fatal("Load succeeded with an undefined identifier")
			}
			if !strings.Contains(err.Error(), "undefinedName") || strings.Contains(err.Error(), "no matching files") {
				t.Errorf("error = %q, want the undefined identifier alone", err)
			}
		})
	}
}

func TestEmbedPattern(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  packages.Error
		want string
		ok   bool
	}{
		{"all prefix", packages.Error{Kind: packages.ListError, Msg: "pattern all:build: no matching files found"}, "all:build", true},
		{"glob", packages.Error{Kind: packages.ListError, Msg: "pattern compliance/build/*: no matching files found"}, "compliance/build/*", true},
		{"type error", packages.Error{Kind: packages.TypeError, Msg: "pattern a: no matching files found"}, "", false},
		{"other embed error", packages.Error{Kind: packages.ListError, Msg: "pattern a: cannot embed directory a: contains no embeddable files"}, "", false},
		{"no pattern", packages.Error{Kind: packages.ListError, Msg: "pattern : no matching files found"}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := embedPattern(tc.err)
			if got != tc.want || ok != tc.ok {
				t.Errorf("embedPattern = %q, %v; want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
