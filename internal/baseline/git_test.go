package baseline

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/metrics"
)

// testRepo is a throwaway git repository under t.TempDir().
type testRepo struct {
	t   *testing.T
	dir string
}

// newRepo initializes an empty repository whose first branch is branch.
func newRepo(t *testing.T, branch string) *testRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	r := &testRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", branch)
	return r
}

// git runs git in the repository with user and system config ignored, so
// the test does not depend on the machine's identity or signing settings.
func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = append(withoutGitOverrides(os.Environ()),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// write creates or replaces the file at rel with content.
func (r *testRepo) write(rel, content string) {
	r.t.Helper()
	path := filepath.Join(r.dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatalf("creating directory for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		r.t.Fatalf("writing %s: %v", rel, err)
	}
}

// commit stages everything and commits it, returning the new commit hash.
func (r *testRepo) commit(msg string) string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "--no-verify", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

// worktrees returns the number of worktrees git lists, the main one included.
func (r *testRepo) worktrees() int {
	r.t.Helper()
	n := 0
	for line := range strings.SplitSeq(r.git("worktree", "list", "--porcelain"), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			n++
		}
	}
	return n
}

// stubExtractor lists fixed packages and records the root it was given. Its
// Extract calls extract when set, and otherwise returns zero metrics.
type stubExtractor struct {
	pkgs    []string
	root    string
	extract func(ctx context.Context) (metrics.RawMetrics, error)
}

func (s *stubExtractor) Language() string   { return "stub" }
func (s *stubExtractor) Detect(string) bool { return true }
func (s *stubExtractor) Packages(root string) ([]string, error) {
	s.root = root
	return s.pkgs, nil
}

func (s *stubExtractor) Extract(ctx context.Context, _ *metrics.ModuleContext, _ string) (metrics.RawMetrics, error) {
	if s.extract == nil {
		return metrics.RawMetrics{}, nil
	}
	return s.extract(ctx)
}

// assertCleanedUp fails unless the repository has only its main worktree and
// the temporary worktree directory is gone.
func assertCleanedUp(t *testing.T, r *testRepo, wtRoot string) {
	t.Helper()
	if wtRoot == "" {
		t.Fatal("extractor was never given a worktree root")
	}
	if n := r.worktrees(); n != 1 {
		t.Errorf("git worktree list shows %d worktrees after FromGit, want 1", n)
	}
	if _, err := os.Stat(wtRoot); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("worktree directory %s still exists (stat err %v)", wtRoot, err)
	}
}

func TestFromGitReturnsBaseCommitMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	r := newRepo(t, "master")
	r.write("go.mod", "module example.com/m\n\ngo 1.27\n")
	r.write("a/a.go", "package a\n\nfunc A() int { return 1 }\n")
	r.write("b/b.go", "package b\n\nvar B = 1\n")
	first := r.commit("first")
	r.write("a/a.go", "package a\n\nvar x = 2\n\nfunc A() int { return x }\n")
	r.commit("second")

	b, err := FromGit(context.Background(), r.dir, first, golang.New(), "example.com/m", "est")
	if err != nil {
		t.Fatalf("FromGit: %v", err)
	}
	if b.Ref() != first {
		t.Errorf("Ref() = %q, want first commit %q", b.Ref(), first)
	}
	if b.Tokenizer() != "est" {
		t.Errorf("Tokenizer() = %q, want the one passed, est", b.Tokenizer())
	}
	for pkg, want := range map[string]int{"example.com/m/a": 0, "example.com/m/b": 1} {
		m, ok := b.Metrics(pkg)
		if !ok {
			t.Errorf("Metrics(%q): not found", pkg)
			continue
		}
		if m.Globals != want {
			t.Errorf("Metrics(%q).Globals = %d, want %d (first commit)", pkg, m.Globals, want)
		}
	}
	if _, ok := b.Metrics("example.com/m/c"); ok {
		t.Error("Metrics of a package absent from the base reported found")
	}
	if n := r.worktrees(); n != 1 {
		t.Errorf("git worktree list shows %d worktrees after FromGit, want 1", n)
	}
}

func TestFromGitUsesMergeBase(t *testing.T) {
	t.Parallel()

	r := newRepo(t, "master")
	r.write("f.txt", "1\n")
	base := r.commit("base")
	r.write("f.txt", "2\n")
	r.commit("master moves on")
	r.git("checkout", "-q", "-b", "feature", base)
	r.write("g.txt", "feature\n")
	r.commit("feature work")

	ext := &stubExtractor{pkgs: []string{"p"}}
	b, err := FromGit(context.Background(), r.dir, "", ext, "example.com/m", "est")
	if err != nil {
		t.Fatalf("FromGit: %v", err)
	}
	if b.Ref() != base {
		t.Errorf("Ref() = %q, want merge-base %q", b.Ref(), base)
	}
	if _, ok := b.Metrics("p"); !ok {
		t.Error(`Metrics("p"): not found`)
	}
	assertCleanedUp(t, r, ext.root)
}

func TestFromGitNestedModule(t *testing.T) {
	t.Parallel()

	r := newRepo(t, "master")
	r.write("tools/go.mod", "module example.com/tools\n")
	sha := r.commit("nested module")

	ext := &stubExtractor{}
	var sawGoMod bool
	ext.extract = func(context.Context) (metrics.RawMetrics, error) {
		_, err := os.Stat(filepath.Join(ext.root, "go.mod"))
		sawGoMod = err == nil
		return metrics.RawMetrics{}, nil
	}
	ext.pkgs = []string{"example.com/tools"}
	if _, err := FromGit(context.Background(), filepath.Join(r.dir, "tools"), sha, ext, "example.com/tools", "est"); err != nil {
		t.Fatalf("FromGit: %v", err)
	}
	if filepath.Base(ext.root) != "tools" {
		t.Errorf("extractor root = %s, want the tools directory inside the worktree", ext.root)
	}
	if !sawGoMod {
		t.Error("extractor root did not contain the nested module's go.mod")
	}
	assertCleanedUp(t, r, filepath.Dir(ext.root))
}

func TestFromGitRemovesWorktreeOnFailure(t *testing.T) {
	t.Parallel()

	errExtract := errors.New("extract failed")
	tests := []struct {
		name    string
		extract func(ctx context.Context, cancel context.CancelFunc) (metrics.RawMetrics, error)
	}{
		{
			name: "extract error",
			extract: func(context.Context, context.CancelFunc) (metrics.RawMetrics, error) {
				return metrics.RawMetrics{}, errExtract
			},
		},
		{
			name: "cancelled context",
			extract: func(ctx context.Context, cancel context.CancelFunc) (metrics.RawMetrics, error) {
				cancel()
				<-ctx.Done()
				return metrics.RawMetrics{}, ctx.Err()
			},
		},
		{
			name: "panic",
			extract: func(context.Context, context.CancelFunc) (metrics.RawMetrics, error) {
				panic("extractor bug")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newRepo(t, "master")
			r.write("f.txt", "1\n")
			sha := r.commit("one")

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ext := &stubExtractor{pkgs: []string{"p"}}
			ext.extract = func(ctx context.Context) (metrics.RawMetrics, error) { return tt.extract(ctx, cancel) }

			err := callRecovering(func() error {
				_, err := FromGit(ctx, r.dir, sha, ext, "example.com/m", "est")
				return err
			})
			if err == nil {
				t.Fatal("FromGit succeeded, want an error")
			}
			assertCleanedUp(t, r, ext.root)
		})
	}
}

// callRecovering runs f and returns its error, or an error describing the
// panic if f panicked.
func callRecovering(f func() error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = errors.New("panic")
		}
	}()
	return f()
}

// TestFromGitRemovesWorktreeOnSignal sends SIGINT to the test process while
// extraction is running. FromGit's signal.NotifyContext must absorb it,
// cancel extraction and remove the worktree. Not parallel: the signal is
// process-wide.
func TestFromGitRemovesWorktreeOnSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cannot send SIGINT to self on windows")
	}

	r := newRepo(t, "master")
	r.write("f.txt", "1\n")
	sha := r.commit("one")

	ext := &stubExtractor{pkgs: []string{"p"}}
	ext.extract = func(ctx context.Context) (metrics.RawMetrics, error) {
		p, err := os.FindProcess(os.Getpid())
		if err != nil {
			return metrics.RawMetrics{}, err
		}
		if err := p.Signal(os.Interrupt); err != nil {
			return metrics.RawMetrics{}, err
		}
		select {
		case <-ctx.Done():
			return metrics.RawMetrics{}, ctx.Err()
		case <-time.After(10 * time.Second):
			return metrics.RawMetrics{}, errors.New("context not cancelled by SIGINT")
		}
	}
	_, err := FromGit(context.Background(), r.dir, sha, ext, "example.com/m", "est")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FromGit error = %v, want context.Canceled from the signal", err)
	}
	assertCleanedUp(t, r, ext.root)
}

func TestDefaultRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		branch  string
		extra   []string // additional refs pointing at HEAD
		want    string
		wantErr bool
	}{
		{name: "only main", branch: "main", want: "main"},
		{name: "only master", branch: "master", want: "master"},
		{name: "master before origin/main", branch: "master", extra: []string{"refs/remotes/origin/main"}, want: "master"},
		{name: "origin/master first", branch: "main", extra: []string{"refs/remotes/origin/master"}, want: "origin/master"},
		{name: "neither", branch: "trunk", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newRepo(t, tt.branch)
			r.write("f.txt", "1\n")
			r.commit("one")
			for _, ref := range tt.extra {
				r.git("update-ref", ref, "HEAD")
			}

			got, err := DefaultRef(context.Background(), r.dir)
			if tt.wantErr {
				if !errors.Is(err, ErrNoDefaultRef) {
					t.Fatalf("DefaultRef error = %v, want ErrNoDefaultRef", err)
				}
				for _, ref := range []string{"origin/master", "master", "origin/main", "main"} {
					if !strings.Contains(err.Error(), ref) {
						t.Errorf("error %q does not name %s", err, ref)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("DefaultRef: %v", err)
			}
			if got != tt.want {
				t.Errorf("DefaultRef = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMergeBaseRejectsBadRefs(t *testing.T) {
	t.Parallel()

	r := newRepo(t, "master")
	r.write("f.txt", "1\n")
	r.commit("one")

	for _, ref := range []string{"--output=/tmp/x", "-h", "", "no-such-branch"} {
		if _, err := MergeBase(context.Background(), r.dir, ref); err == nil {
			t.Errorf("MergeBase(%q) succeeded, want an error", ref)
		}
	}
}

func TestDefaultRefOutsideRepository(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	_, err := DefaultRef(context.Background(), t.TempDir())
	if err == nil || errors.Is(err, ErrNoDefaultRef) {
		t.Errorf("DefaultRef outside a repository = %v, want a git error", err)
	}
}

// rootRecorder wraps an extractor and records the root Packages was given.
// It does not implement metrics.Forgetter, so FromGit cannot release its
// loads.
type rootRecorder struct {
	metrics.Extractor
	root string
}

func (r *rootRecorder) Packages(root string) ([]string, error) {
	r.root = root
	return r.Extractor.Packages(root)
}

// forgettingRecorder is a rootRecorder that forwards Forget to the wrapped
// Go extractor and records each root it was given.
type forgettingRecorder struct {
	rootRecorder
	forgot []string
}

func (f *forgettingRecorder) Forget(root string) {
	f.forgot = append(f.forgot, root)
	if g, ok := f.Extractor.(metrics.Forgetter); ok {
		g.Forget(root)
	}
}

// twoCommitRepo returns a repository with a two-package Go module committed
// twice, and the first commit's hash.
func twoCommitRepo(t *testing.T) (r *testRepo, first string) {
	t.Helper()
	r = newRepo(t, "master")
	r.write("go.mod", "module example.com/m\n\ngo 1.27\n")
	r.write("a/a.go", "package a\n\nimport \"fmt\"\n\nfunc A() string { return fmt.Sprint(1) }\n")
	r.write("b/b.go", "package b\n\nvar B = 1\n")
	first = r.commit("first")
	r.write("a/a.go", "package a\n\nvar x = 2\n\nfunc A() int { return x }\n")
	r.commit("second")
	return r, first
}

func TestFromGitForgetsWorktreeLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	r, first := twoCommitRepo(t)
	ext := &forgettingRecorder{rootRecorder: rootRecorder{Extractor: golang.New()}}
	if _, err := FromGit(context.Background(), r.dir, first, ext, "example.com/m", "est"); err != nil {
		t.Fatalf("FromGit: %v", err)
	}
	if len(ext.forgot) != 1 || ext.forgot[0] != ext.root {
		t.Fatalf("Forget called with %q, want once with the worktree root %q", ext.forgot, ext.root)
	}
	assertCleanedUp(t, r, ext.root)
	// With the load forgotten, listing the deleted worktree must load it
	// again and fail; a cached load would still answer.
	if pkgs, err := ext.Extractor.Packages(ext.root); err == nil {
		t.Errorf("Packages on the removed worktree = %v, want a load error (load still cached)", pkgs)
	}
}

// TestFromGitForgetHeap measures the heap a baseline's load holds when
// FromGit's extractor cannot release it, by forgetting it afterwards. The
// figure is informational; run with -v to see it.
func TestFromGitForgetHeap(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	// Not parallel, so other tests' allocations blur the figure less.
	r, first := twoCommitRepo(t)
	gox := golang.New()
	ext := &rootRecorder{Extractor: gox}
	if _, err := FromGit(context.Background(), r.dir, first, ext, "example.com/m", "est"); err != nil {
		t.Fatalf("FromGit: %v", err)
	}
	var held, released runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&held)
	gox.Forget(ext.root)
	runtime.GC()
	runtime.ReadMemStats(&released)
	t.Logf("HeapAlloc with the baseline load held %d B, after Forget %d B, delta %d B",
		held.HeapAlloc, released.HeapAlloc, int64(held.HeapAlloc)-int64(released.HeapAlloc))
	if _, err := gox.Packages(ext.root); err == nil {
		t.Error("Packages on the removed worktree succeeded after Forget, want a reload that fails")
	}
}
