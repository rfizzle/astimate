package load

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/exclude"
	"golang.org/x/tools/go/packages"
)

// TestExclude checks Module.Exclude on a module load: a package whose
// non-test files the list all matches leaves Paths and Pkgs for Skipped
// with the first pattern, so does one that fails to load, a package with
// one file excluded keeps the rest, a test variant left with no test file
// is dropped, and each excluded file is logged at debug level.
func TestExclude(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":                "module example.com/x\n\ngo 1.22\n",
		"gen/gen.go":            "package gen\n\n// G is one.\nconst G = 1\n",
		"broken/broken.go":      "package broken\n\nimport _ \"example.com/absent/client\"\n",
		"part/a.go":             "package part\n\n// A is one.\nconst A = 1\n",
		"part/b_gen.go":         "package part\n\n// B is two.\nconst B = 2\n",
		"part/b_gen_test.go":    "package part\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) { _ = B }\n",
		"pure/pure.go":          "package pure\n\n// P is one.\nconst P = 1\n",
		"pure/pure_test.go":     "package pure\n\nimport \"testing\"\n\nfunc TestP(t *testing.T) { _ = P }\n",
		"pure/pure_ext_test.go": "package pure_test\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, p, body)
	}
	m, err := Load(&packages.Config{Dir: root}, packages.Load)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.SkippedPackages) != 1 || m.SkippedPackages[0].Package != "example.com/x/broken" {
		t.Fatalf("SkippedPackages = %+v, want broken", m.SkippedPackages)
	}
	list, err := exclude.New([]string{"gen/**", "broken", "**/*_gen.go", "**/*_gen_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	m.Exclude(root, list, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))

	if want := []string{"example.com/x/part", "example.com/x/pure"}; !slices.Equal(m.Paths, want) {
		t.Errorf("Paths = %v, want %v", m.Paths, want)
	}
	if _, ok := m.Pkgs["example.com/x/gen"]; ok {
		t.Error("gen is still in Pkgs")
	}
	wantSkipped := []SkippedDir{
		{Dir: "broken", ImportPath: "example.com/x/broken", Reason: "excluded by config (broken)"},
		{Dir: "gen", ImportPath: "example.com/x/gen", Reason: "excluded by config (gen/**)"},
	}
	if !slices.Equal(m.Skipped, wantSkipped) {
		t.Errorf("Skipped = %+v, want %+v", m.Skipped, wantSkipped)
	}
	if len(m.SkippedPackages) != 0 {
		t.Errorf("SkippedPackages = %+v, want none: broken is excluded", m.SkippedPackages)
	}
	part := m.Pkgs["example.com/x/part"]
	if len(part.GoFiles) != 1 || len(part.Syntax) != 1 || filepath.Base(part.GoFiles[0]) != "a.go" {
		t.Errorf("part files = %v, want a.go alone", part.GoFiles)
	}
	if _, ok := m.Tests["example.com/x/part"]; ok {
		t.Error("part keeps a test variant whose only test file is excluded")
	}
	if len(m.TestPackages(m.Pkgs["example.com/x/pure"])) != 2 {
		t.Error("pure lost a test variant, though none of its files is excluded")
	}
	for _, f := range []string{"part/b_gen.go", "part/b_gen_test.go", "gen/gen.go", "broken/broken.go"} {
		if !strings.Contains(logs.String(), "file="+f+" ") {
			t.Errorf("log does not name the excluded file %s:\n%s", f, logs.String())
		}
	}

	nothing := loadRoot(t, root)
	before := len(nothing.Paths)
	nothing.Exclude(root, nil, nil)
	if len(nothing.Paths) != before {
		t.Error("a nil list excluded something")
	}
}
