package engine

import (
	"bytes"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitIn runs git with args in dir, failing t on error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	env := make([]string, 0, len(os.Environ())+7)
	for _, kv := range os.Environ() {
		// A git hook's variables would redirect git away from dir.
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	env = append(env,
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// writeFiles writes files, keyed by slash path, below root.
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// assertNoTempPath fails t when text names a temporary directory.
func assertNoTempPath(t *testing.T, what, text string) {
	t.Helper()
	tmps := []string{os.TempDir(), "astimate-staged-", "astimate-baseline-"}
	if resolved, err := filepath.EvalSymlinks(os.TempDir()); err == nil {
		tmps = append(tmps, resolved)
	}
	for _, tmp := range tmps {
		if strings.Contains(text, tmp) {
			t.Errorf("%s names temporary path %q:\n%s", what, tmp, text)
		}
	}
}

// TestCheckTempTreeErrorPaths checks that an extraction error from a
// temporary tree, the staged index copy or the baseline worktree, names
// paths relative to the module rather than the temporary directory.
func TestCheckTempTreeErrorPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	const goMod = "module example.com/tt\n\ngo 1.22\n"
	const good = "package tt\n\n// F is exported.\nfunc F() int { return 1 }\n"
	const broken = "package bad\n\nfunc {\n"

	t.Run("staged", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		writeFiles(t, root, map[string]string{"go.mod": goMod, "tt.go": good})
		gitIn(t, root, "init", "-q", "-b", "master")
		gitIn(t, root, "add", ".")
		gitIn(t, root, "commit", "-q", "-m", "init")
		writeFiles(t, root, map[string]string{"bad/bad.go": broken})
		gitIn(t, root, "add", "bad/bad.go")
		// Only the index holds the broken file now.
		if err := os.Remove(filepath.Join(root, "bad", "bad.go")); err != nil {
			t.Fatal(err)
		}

		tg, err := LoadTarget(root, TargetOptions{})
		if err != nil {
			t.Fatalf("LoadTarget: %v", err)
		}
		var logs bytes.Buffer
		tg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
		_, failed, err := Check(t.Context(), tg, CheckOptions{Base: "master", Staged: true})
		var text string
		switch {
		case err != nil:
			text = err.Error()
		case len(failed) == 0:
			t.Fatalf("Check reported no failure for the broken staged package; logs:\n%s", logs.String())
		default:
			for _, f := range failed {
				text += f.Error() + "\n"
			}
			text += logs.String()
		}
		if !strings.Contains(text, filepath.Join("bad", "bad.go")) {
			t.Errorf("error does not name bad/bad.go relative to the module:\n%s", text)
		}
		assertNoTempPath(t, "staged check error", text)
	})

	t.Run("git baseline", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		writeFiles(t, root, map[string]string{"go.mod": goMod, "tt.go": good, "bad/bad.go": broken})
		gitIn(t, root, "init", "-q", "-b", "master")
		gitIn(t, root, "add", ".")
		gitIn(t, root, "commit", "-q", "-m", "broken base")
		writeFiles(t, root, map[string]string{"bad/bad.go": "package bad\n"})

		tg, err := LoadTarget(root, TargetOptions{})
		if err != nil {
			t.Fatalf("LoadTarget: %v", err)
		}
		tg.Logger = slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
		_, _, err = Check(t.Context(), tg, CheckOptions{Base: "master", All: true})
		if err == nil {
			t.Fatal("Check succeeded against a broken baseline, want an extraction error")
		}
		if !strings.Contains(err.Error(), filepath.Join("bad", "bad.go")) {
			t.Errorf("error does not name bad/bad.go relative to the module: %v", err)
		}
		assertNoTempPath(t, "baseline error", err.Error())
	})
}
