package golang

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestModulePath(t *testing.T) {
	got, err := ModulePath(fixtureRoot(t))
	if err != nil {
		t.Fatalf("ModulePath: %v", err)
	}
	if got != "example.com/fixture" {
		t.Fatalf("ModulePath = %q, want example.com/fixture", got)
	}
	if _, err := ModulePath(t.TempDir()); err == nil {
		t.Fatal("ModulePath on a directory without go.mod: want error")
	}
}

func TestFindModuleRoot(t *testing.T) {
	root := fixtureRoot(t)
	nested := filepath.Join(root, "hub")
	got, err := FindModuleRoot(nested)
	if err != nil {
		t.Fatalf("FindModuleRoot: %v", err)
	}
	if got != root {
		t.Fatalf("FindModuleRoot(%s) = %s, want %s", nested, got, root)
	}

	// A go.mod in an ancestor outside any module must not be found from an
	// isolated temp tree; the temp root has none.
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = FindModuleRoot(filepath.Join(tmp, "a", "b"))
	if !errors.Is(err, ErrNoModule) {
		t.Fatalf("FindModuleRoot outside a module: err = %v, want ErrNoModule", err)
	}
}
