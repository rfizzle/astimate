package action_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runScript runs the bash script name from this directory with env added to
// a minimal environment and returns its exit code, stdout and stderr.
func runScript(t *testing.T, name string, env []string) (code int, stdout, stderr string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "bash", abs)
	cmd.Dir = t.TempDir()
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}, env...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("running %s: %v", name, err)
	}
	return code, out.String(), errOut.String()
}

// TestInstallRelease runs install.sh against the recorded release in
// testdata/releases, whose checksums.txt is correct for linux_amd64, wrong
// for linux_arm64 and silent on darwin_arm64.
func TestInstallRelease(t *testing.T) {
	for _, tool := range []string{"curl", "tar", "awk"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	releases, err := filepath.Abs(filepath.Join("testdata", "releases"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		version    string
		os, arch   string
		wantOK     bool
		wantStderr string
	}{
		{name: "checksum matches", version: "v0.0.0-test", os: "Linux", arch: "X64", wantOK: true},
		{name: "checksum mismatch", version: "v0.0.0-test", os: "Linux", arch: "ARM64",
			wantStderr: "checksum verification failed for astimate_0.0.0-test_linux_arm64.tar.gz"},
		{name: "no checksum line", version: "v0.0.0-test", os: "macOS", arch: "ARM64",
			wantStderr: "no checksum for astimate_0.0.0-test_darwin_arm64.tar.gz"},
		{name: "unknown tag", version: "v9.9.9", os: "Linux", arch: "X64",
			wantStderr: "cannot download"},
		{name: "unsupported os", version: "v0.0.0-test", os: "Windows", arch: "X64",
			wantStderr: "unsupported runner OS Windows"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			temp := t.TempDir()
			ghPath := filepath.Join(temp, "github_path")
			code, stdout, stderr := runScript(t, "install.sh", []string{
				"ASTIMATE_VERSION=" + tt.version,
				"ASTIMATE_RELEASE_URL=file://" + releases,
				"ACTION_DIR=" + temp,
				"RUNNER_TEMP=" + temp,
				"RUNNER_OS=" + tt.os,
				"RUNNER_ARCH=" + tt.arch,
				"GITHUB_PATH=" + ghPath,
			})
			bin := filepath.Join(temp, "astimate", "astimate")
			if !tt.wantOK {
				if code == 0 {
					t.Fatalf("install succeeded, want failure\nstdout: %s", stdout)
				}
				if !strings.Contains(stderr, tt.wantStderr) {
					t.Errorf("stderr = %q, want it to contain %q", stderr, tt.wantStderr)
				}
				if _, err := os.Stat(bin); err == nil {
					t.Errorf("%s exists after a failed install", bin)
				}
				if _, err := os.Stat(ghPath); err == nil {
					t.Errorf("GITHUB_PATH written after a failed install")
				}
				return
			}
			if code != 0 {
				t.Fatalf("install exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			out, err := exec.CommandContext(t.Context(), bin, "version").Output()
			if err != nil {
				t.Fatalf("installed astimate: %v", err)
			}
			if !strings.Contains(string(out), "version: v0.0.0-test") {
				t.Errorf("installed astimate version output = %q", out)
			}
			path, err := os.ReadFile(ghPath)
			if err != nil {
				t.Fatalf("reading GITHUB_PATH: %v", err)
			}
			if got, want := strings.TrimSpace(string(path)), filepath.Join(temp, "astimate"); got != want {
				t.Errorf("GITHUB_PATH = %q, want %q", got, want)
			}
		})
	}
}

// TestInstallSource builds astimate from this repository the way the
// repository's own CI does with version: source.
func TestInstallSource(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary; skipped under -short")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	actionDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	temp := t.TempDir()
	code, stdout, stderr := runScript(t, "install.sh", []string{
		"ASTIMATE_VERSION=source",
		"ACTION_DIR=" + actionDir,
		"RUNNER_TEMP=" + temp,
		"GOCACHE=" + goEnv(t, "GOCACHE"),
		"GOMODCACHE=" + goEnv(t, "GOMODCACHE"),
	})
	if code != 0 {
		t.Fatalf("install exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	out, err := exec.CommandContext(t.Context(), filepath.Join(temp, "astimate", "astimate"), "version").Output()
	if err != nil {
		t.Fatalf("built astimate: %v", err)
	}
	if !strings.Contains(string(out), "date: ") || strings.Contains(string(out), "date: unknown") {
		t.Errorf("version output %q lacks the injected build date", out)
	}
}

// goEnv returns `go env key`, so the source build reuses the test's caches.
func goEnv(t *testing.T, key string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "env", key).Output()
	if err != nil {
		t.Fatalf("go env %s: %v", key, err)
	}
	return strings.TrimSpace(string(out))
}

// moduleBelowRoot creates a git repository with one commit and returns the
// absolute path of the directory services/api inside it, a module root
// below the repository root. It skips the test when git is missing.
func moduleBelowRoot(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()
	module := filepath.Join(repo, "services", "api")
	if err := os.MkdirAll(module, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module example.com/api\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"commit", "-q", "--no-verify", "-m", "init"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return module
}

// TestRun runs run.sh with a fake astimate on PATH that prints its
// arguments and exits with FAKE_EXIT, outside any git repository.
func TestRun(t *testing.T) {
	bin := t.TempDir()
	fake := "#!/bin/sh\necho \"::error file=pkg::args $*\"\nexit \"${FAKE_EXIT:-0}\"\n"
	if err := os.WriteFile(filepath.Join(bin, "astimate"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	module := moduleBelowRoot(t)
	tests := []struct {
		name       string
		env        []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "pass", env: []string{"FAKE_EXIT=0"},
			wantStdout: "::error file=pkg::args check --format github -- .\n"},
		{name: "gate failed", env: []string{"FAKE_EXIT=3", "ASTIMATE_ALL=true", "ASTIMATE_CONFIG=a.yaml"},
			wantCode: 3, wantStdout: "::error file=pkg::args check --format github --all --config a.yaml -- .\n",
			wantStderr: "the gate failed (exit 3)"},
		// The base ref is looked up in the module's repository, not the
		// working directory's, which is in none.
		{name: "path input", env: []string{"ASTIMATE_PATH=" + module, "ASTIMATE_BASE=HEAD"},
			wantStdout: "::error file=pkg::args check --format github --base HEAD -- " + module + "\n"},
		{name: "missing path", env: []string{"ASTIMATE_PATH=" + filepath.Join(module, "absent")},
			wantCode: 2, wantStderr: "input path " + filepath.Join(module, "absent") + " is not a directory"},
		{name: "analysis failed", env: []string{"FAKE_EXIT=2"}, wantCode: 2, wantStderr: "analysis failed (exit 2)"},
		{name: "bad all", env: []string{"ASTIMATE_ALL=yes"}, wantCode: 2, wantStderr: "input all must be true or false"},
		{name: "missing base", env: []string{"ASTIMATE_BASE=origin/master", "GIT_CEILING_DIRECTORIES=/"},
			wantCode: 2, wantStderr: "base ref origin/master not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := append([]string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")}, tt.env...)
			code, stdout, stderr := runScript(t, "run.sh", env)
			if code != tt.wantCode {
				t.Errorf("exit = %d, want %d\nstderr: %s", code, tt.wantCode, stderr)
			}
			if tt.wantStdout != "" && stdout != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout, tt.wantStdout)
			}
			if !strings.Contains(stderr, tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tt.wantStderr)
			}
		})
	}
}
