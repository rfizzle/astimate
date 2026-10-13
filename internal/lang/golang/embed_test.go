package golang

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// embedModule writes a module whose package web embeds "all:build", with
// the build directory present when withBuild is set, and returns its root.
func embedModule(t *testing.T, withBuild bool) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/emb\n\ngo 1.22\n",
		"web/web.go": "package web\n\nimport \"embed\"\n\n//go:embed all:build\nvar FS embed.FS\n\n" +
			"// Serve returns the files.\nfunc Serve() embed.FS { return FS }\n",
	}
	if withBuild {
		files["web/build/index.html"] = "<html>"
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		writeFile(t, path, content)
	}
	return root
}

// TestEmbedWithoutFiles checks that a package embedding a directory the
// checkout lacks reports the metrics it reports with the directory, and logs
// one warning naming the package and the pattern.
func TestEmbedWithoutFiles(t *testing.T) {
	extract := func(root string, logger *slog.Logger) metrics.RawMetrics {
		t.Helper()
		e := New(WithLogger(logger))
		raw, err := e.Extract(context.Background(), &metrics.ModuleContext{Root: root}, "example.com/emb/web")
		if err != nil {
			t.Fatalf("Extract: %v", err)
		}
		return raw
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	missing := extract(embedModule(t, false), logger)
	present := extract(embedModule(t, true), slog.New(slog.NewTextHandler(&buf, nil)))
	if !reflect.DeepEqual(missing, present) {
		t.Errorf("metrics without the directory = %+v, with it = %+v", missing, present)
	}
	got := buf.String()
	if n := strings.Count(got, "level=WARN"); n != 1 {
		t.Fatalf("warnings = %d, want 1:\n%s", n, got)
	}
	for _, want := range []string{"package=example.com/emb/web", "pattern=all:build"} {
		if !strings.Contains(got, want) {
			t.Errorf("log lacks %q:\n%s", want, got)
		}
	}
}

// TestUndefinedIdentifierFails checks that an error other than an unmatched
// embed pattern still fails the load.
func TestUndefinedIdentifierFails(t *testing.T) {
	root := embedModule(t, false)
	writeFile(t, filepath.Join(root, "web", "bad.go"), "package web\n\nvar _ = undefinedName\n")
	_, err := New().Packages(root)
	if err == nil || !strings.Contains(err.Error(), "undefinedName") {
		t.Fatalf("Packages error = %v, want one naming undefinedName", err)
	}
}
