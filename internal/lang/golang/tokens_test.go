package golang

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"
)

// knownSnippet is a fixed Go snippet for the exact-count test.
const knownSnippet = "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello, world\")\n}\n"

// knownSnippetO200k is the o200k_base token count of knownSnippet, captured
// from tiktoken-go v0.1.8 with tiktoken-go-loader v0.0.2.
const knownSnippetO200k = 19

// newO200kForTest returns an o200k counter with proxies pointed at an
// unroutable address and an empty tiktoken cache directory, so a download of
// the BPE file would fail instead of succeeding.
func newO200kForTest(t *testing.T) tokenCounter {
	t.Helper()
	t.Setenv("TIKTOKEN_CACHE_DIR", t.TempDir())
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		t.Setenv(k, "http://127.0.0.1:9")
	}
	for _, k := range []string{"NO_PROXY", "no_proxy"} {
		t.Setenv(k, "")
	}
	c, err := newO200kCounter()
	if err != nil {
		t.Fatalf("newO200kCounter: %v", err)
	}
	return c
}

// writeTokenFile writes data to name in dir and returns its path.
func writeTokenFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("writing %s: %v", p, err)
	}
	return p
}

func TestRatioCounterArithmetic(t *testing.T) {
	dir := t.TempDir()
	f400 := writeTokenFile(t, dir, "f400.go", make([]byte, 400))
	f5 := writeTokenFile(t, dir, "f5.go", make([]byte, 5))
	f6 := writeTokenFile(t, dir, "f6.go", make([]byte, 6))
	empty := writeTokenFile(t, dir, "empty.go", nil)

	for _, tc := range []struct {
		name  string
		ratio float64
		paths []string
		want  int
	}{
		{"400 bytes at 4.0", 4.0, []string{f400}, 100},
		{"400 bytes at default", defaultCharsPerToken, []string{f400}, 125},
		// Summed first: 11/3.2 = 3.4 -> 3. Per file it would be 1+1 = 2.
		{"bytes summed before dividing", defaultCharsPerToken, []string{f5, f6}, 3},
		{"truncates toward zero", 4.0, []string{f6}, 1},
		{"empty file", 4.0, []string{empty}, 0},
		{"no files", 4.0, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := newRatioCounter(tc.ratio).Count(tc.paths)
			if err != nil {
				t.Fatalf("Count: %v", err)
			}
			if got != tc.want {
				t.Errorf("Count = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRatioCounterErrors(t *testing.T) {
	f := writeTokenFile(t, t.TempDir(), "f.go", []byte("package f\n"))
	for _, tc := range []struct {
		name  string
		ratio float64
		paths []string
	}{
		{"zero ratio", 0, []string{f}},
		{"negative ratio", -1, []string{f}},
		{"missing file", 4.0, []string{filepath.Join(t.TempDir(), "missing.go")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := newRatioCounter(tc.ratio).Count(tc.paths); err == nil {
				t.Error("Count succeeded, want error")
			}
		})
	}
}

func TestO200kKnownString(t *testing.T) {
	c := newO200kForTest(t)
	f := writeTokenFile(t, t.TempDir(), "main.go", []byte(knownSnippet))
	got, err := c.Count([]string{f, f})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if got != 2*knownSnippetO200k {
		t.Errorf("Count of two copies = %d, want %d", got, 2*knownSnippetO200k)
	}
	if _, err := c.Count([]string{filepath.Join(t.TempDir(), "missing.go")}); err == nil {
		t.Error("Count of a missing file succeeded, want error")
	}
}

func TestO200kOffline(t *testing.T) {
	start := time.Now()
	c := newO200kForTest(t)
	// Cold construction measured about 0.3 to 0.45s under -race; the bound
	// leaves headroom for a loaded machine.
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("newO200kCounter took %v, want well under a second offline", d)
	}
	if got := c.Method(); got != methodO200k {
		t.Errorf("Method = %q, want %q", got, methodO200k)
	}
}

// TestRatioNearO200k documents o200k_base's density on Go: a ratio of 4.0
// bytes per token matches it within 5% on hub's non-test files (679 bytes:
// 169 estimated, 170 exact). The default, defaultCharsPerToken, does not
// target o200k but the denser Anthropic tokenizers (SPEC.md 6.1), so it is
// deliberately not compared here.
func TestRatioNearO200k(t *testing.T) {
	const o200kCharsPerToken = 4.0
	l := loadFixture(t)
	hub := l.pkgs["example.com/fixture/hub"]
	est, err := newRatioCounter(o200kCharsPerToken).Count(hub.GoFiles)
	if err != nil {
		t.Fatalf("ratio Count: %v", err)
	}
	exact, err := newO200kForTest(t).Count(hub.GoFiles)
	if err != nil {
		t.Fatalf("o200k Count: %v", err)
	}
	diff := float64(est-exact) / float64(exact)
	t.Logf("hub: ratio %v est %d, o200k %d, diff %+.1f%%", o200kCharsPerToken, est, exact, diff*100)
	if diff < -0.05 || diff > 0.05 {
		t.Errorf("ratio %v estimate %d is %.1f%% from o200k %d, want within 5%%", o200kCharsPerToken, est, diff*100, exact)
	}
}

func TestTokensWithTests(t *testing.T) {
	l := loadFixture(t)
	p := l.pkgs["example.com/fixture/tested"]
	got, err := tokens(l, p, newRatioCounter(1))
	if err != nil {
		t.Fatalf("tokens: %v", err)
	}

	sum := func(pred func(string) bool) int {
		entries, err := os.ReadDir(p.Dir)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".go") || !pred(e.Name()) {
				continue
			}
			fi, err := e.Info()
			if err != nil {
				t.Fatal(err)
			}
			n += int(fi.Size())
		}
		return n
	}
	isTest := func(name string) bool { return strings.HasSuffix(name, "_test.go") }
	nonTest := sum(func(n string) bool { return !isTest(n) })
	all := sum(func(string) bool { return true })
	if nonTest == all {
		t.Fatal("fixture tested has no _test.go bytes")
	}

	want := tokenCounts{tokensEst: nonTest, tokensEstWithTests: all, method: methodEst}
	if got != want {
		t.Errorf("tokens at 1 byte/token = %+v, want %+v", got, want)
	}

	// Without test variants both totals are the non-test total.
	bare := &loaded{fset: l.fset, pkgs: l.pkgs}
	got, err = tokens(bare, p, newRatioCounter(1))
	if err != nil {
		t.Fatalf("tokens without tests: %v", err)
	}
	if got.tokensEstWithTests != nonTest {
		t.Errorf("tokens_est_with_tests without test variants = %d, want %d", got.tokensEstWithTests, nonTest)
	}
}

func TestTokensErrors(t *testing.T) {
	p := &packages.Package{PkgPath: "example.com/x", GoFiles: []string{filepath.Join(t.TempDir(), "gone.go")}}
	if _, err := tokens(&loaded{}, p, newRatioCounter(defaultCharsPerToken)); err == nil ||
		!strings.Contains(err.Error(), "example.com/x") {
		t.Errorf("tokens error = %v, want one naming the package", err)
	}
}

// TestTokensFixtureMethods checks, for both counters, the recorded method
// and that the with-tests total never falls below the non-test one. The
// conformance suite covers the "est" values against the goldens.
func TestTokensFixtureMethods(t *testing.T) {
	l := loadFixture(t)
	for _, c := range []tokenCounter{newRatioCounter(defaultCharsPerToken), newO200kForTest(t)} {
		for _, pkg := range l.paths {
			t.Run(c.Method()+"/"+path.Base(pkg), func(t *testing.T) {
				tc, err := tokens(l, l.pkgs[pkg], c)
				if err != nil {
					t.Fatal(err)
				}
				if tc.method != c.Method() {
					t.Errorf("method = %q, want %q", tc.method, c.Method())
				}
				if tc.tokensEstWithTests < tc.tokensEst {
					t.Errorf("tokens_est_with_tests %d < tokens_est %d", tc.tokensEstWithTests, tc.tokensEst)
				}
			})
		}
	}
}
