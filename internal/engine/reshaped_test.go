package engine

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/report"
)

// exportsSource returns package pkg with one const block of n exported
// names, E00 onwards, then a blank line and extra, which starts on line
// n+6. Fewer than 60 names hold no duplicate block of the default
// duplication.min_tokens, and no exported function needs a test.
func exportsSource(pkg string, n int, extra string) string {
	var b strings.Builder
	b.WriteString("package " + pkg + "\n\nconst (\n")
	for i := range n {
		name := strconv.Itoa(i)
		if i < 10 {
			name = "0" + name
		}
		b.WriteString("\tE" + name)
		if i == 0 {
			b.WriteString(" = iota")
		}
		b.WriteString("\n")
	}
	return b.String() + ")\n\n" + extra
}

// checkDefault checks the module at dir against the tag base under the
// embedded default configuration, the one astimate ships.
func checkDefault(t *testing.T, dir string) *report.Check {
	t.Helper()
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	tg, err := LoadTarget(dir, TargetOptions{Config: cfg, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatalf("LoadTarget: %v", err)
	}
	c, failed, err := Check(t.Context(), tg, CheckOptions{Base: "base"})
	if err != nil || len(failed) != 0 {
		t.Fatalf("Check = (%v, %v), want no error", failed, err)
	}
	return c
}

// reshapedHeadsUps returns the reshaped-exports heads-ups among r's
// warnings, failing t when one is a violation or an exemption.
func reshapedHeadsUps(t *testing.T, r *report.Report) []report.Finding {
	t.Helper()
	var out []report.Finding
	for _, f := range r.Warnings {
		if gate.IsReshaped(f.Limit) {
			out = append(out, f)
		}
	}
	for _, f := range r.Violations {
		if gate.IsReshaped(f.Limit) {
			t.Errorf("heads-up reported as a violation: %+v", f)
		}
	}
	return out
}

// TestCheckReshapedExports checks the reshaped-exports heads-up end to end
// on the Go extractor and a git baseline (SPEC.md 8.1): it names and
// locates an export whose signature changed or that was made unexported in
// a package at the exported_symbols ceiling, never fails the check, and is
// not raised below the warn band, for a deleted export, for a new package
// or for a main package, which the default rule does not judge.
func TestCheckReshapedExports(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	tests := []struct {
		name       string
		base, head map[string]string
		pkg        string
		// want is the heads-up's list of names, loc its location; empty
		// for none.
		want, loc string
	}{
		{
			name: "variadic added at the ceiling", pkg: "p",
			base: map[string]string{"p/p.go": exportsSource("p", 59, "func SetLatest(name string) {}\n")},
			head: map[string]string{"p/p.go": exportsSource("p", 59, "func SetLatest(name string, pin ...string) {}\n")},
			want: "SetLatest (signature changed)", loc: "p/p.go:65",
		},
		{
			name: "variadic added below the warn band", pkg: "p",
			base: map[string]string{"p/p.go": exportsSource("p", 10, "func SetLatest(name string) {}\n")},
			head: map[string]string{"p/p.go": exportsSource("p", 10, "func SetLatest(name string, pin ...string) {}\n")},
		},
		{
			name: "type unexported at the ceiling", pkg: "p",
			base: map[string]string{"p/p.go": exportsSource("p", 59, "type Options struct{}\n")},
			head: map[string]string{"p/p.go": exportsSource("p", 59, "func Extra() {}\n\ntype options struct{}\n")},
			want: "Options (unexported as options)", loc: "p/p.go:67",
		},
		{
			name: "type deleted at the ceiling", pkg: "p",
			base: map[string]string{"p/p.go": exportsSource("p", 59, "type Options struct{}\n")},
			head: map[string]string{"p/p.go": exportsSource("p", 59, "func Extra() {}\n")},
		},
		{
			name: "new package at the ceiling", pkg: "q",
			base: map[string]string{"p/p.go": exportsSource("p", 1, "")},
			head: map[string]string{"q/q.go": exportsSource("q", 59, "func SetLatest(name string, pin ...string) {}\n")},
		},
		{
			name: "main package at the ceiling", pkg: "cmd",
			base: map[string]string{"cmd/main.go": exportsSource("main", 59, "func SetLatest(name string) {}\n\nfunc main() {}\n")},
			head: map[string]string{"cmd/main.go": exportsSource("main", 59, "func SetLatest(name string, pin ...string) {}\n\nfunc main() {}\n")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			base := map[string]string{"go.mod": "module example.com/m\n\ngo 1.22\n"}
			for k, v := range tt.base {
				base[k] = v
			}
			dir := gitRepoAt(t, base)
			for k := range tt.base {
				if _, kept := tt.head[k]; !kept {
					gitIn(t, dir, "rm", "-q", k)
				}
			}
			writeFiles(t, dir, tt.head)
			gitIn(t, dir, "add", "-A")
			gitIn(t, dir, "commit", "-q", "--no-verify", "-m", "change")

			c := checkDefault(t, dir)
			r := findRow(c, tt.pkg)
			if r == nil {
				t.Fatalf("checked %d packages, want %s", len(c.Packages), tt.pkg)
			}
			if c.Failed() {
				t.Errorf("check failed, want it to pass: %+v", r.Violations)
			}
			got := reshapedHeadsUps(t, r)
			if tt.want == "" {
				if len(got) != 0 {
					t.Errorf("heads-ups = %+v, want none", got)
				}
				return
			}
			if len(got) != 1 || !strings.Contains(got[0].Suggestion, "Reshaped without adding an export: "+tt.want+".") {
				t.Fatalf("heads-ups = %+v, want one naming %s", got, tt.want)
			}
			if l := got[0].Location; l == nil || l.File+":"+strconv.Itoa(l.Line) != tt.loc {
				t.Errorf("heads-up located at %+v, want %s", l, tt.loc)
			}
		})
	}
}

// TestCheckReshapedOutputs checks that the heads-up reaches the hook and
// JSON formats as a warning: in the hook's stderr on a passing check, and
// in the JSON's warnings with passed true.
func TestCheckReshapedOutputs(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	dir := gitRepoAt(t, map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.22\n",
		"p/p.go": exportsSource("p", 59, "func SetLatest(name string) {}\n"),
	})
	writeFiles(t, dir, map[string]string{"p/p.go": exportsSource("p", 59, "func SetLatest(name string, pin ...string) {}\n")})
	gitIn(t, dir, "commit", "-q", "--no-verify", "-am", "change")
	c := checkDefault(t, dir)
	if c.Failed() {
		t.Fatalf("check failed, want it to pass: %+v", c)
	}

	var hook, stderr bytes.Buffer
	if err := report.WriteHook(&hook, &stderr, c); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(hook.String()) != "{}" ||
		!strings.Contains(stderr.String(), "exported_symbols: 60 -> 60, exports reshaped, max 60. Reshaped without adding an export: SetLatest (signature changed).") {
		t.Errorf("hook = %q, stderr = %q, want {} and the heads-up on stderr", hook.String(), stderr.String())
	}

	var out bytes.Buffer
	if err := report.WriteCheckJSON(&out, c); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		PackagePath string           `json:"package_path"`
		Passed      bool             `json:"passed"`
		Warnings    []report.Finding `json:"warnings"`
	}
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		for _, w := range r.Warnings {
			if r.PackagePath == "p" && r.Passed && w.Metric == "exported_symbols" && w.Limit == "exports reshaped, max 60" &&
				w.Location != nil && w.Location.File == "p/p.go" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("JSON has no passing row p with the heads-up among its warnings:\n%s", out.String())
	}
}

// TestCheckReshapedBaselineFile checks the heads-up against a baseline
// file: one written now records exports and signatures, so the heads-up
// appears; one written before they existed loads without an error or a
// warning and yields none.
func TestCheckReshapedBaselineFile(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	dir := gitRepoAt(t, map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.22\n",
		"p/p.go": exportsSource("p", 58, "func SetLatest(name string) {}\n\ntype Options struct{}\n"),
	})
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	tg, err := LoadTarget(dir, TargetOptions{Config: cfg, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(t.TempDir(), "baseline.json")
	if _, _, err := WriteBaseline(t.Context(), tg, fresh); err != nil {
		t.Fatalf("WriteBaseline: %v", err)
	}
	old := filepath.Join(t.TempDir(), "old.json")
	writeWithout(t, fresh, old)

	writeFiles(t, dir, map[string]string{
		"p/p.go": exportsSource("p", 58, "func SetLatest(name string, pin ...string) {}\n\nfunc Extra() {}\n\ntype options struct{}\n"),
	})
	gitIn(t, dir, "commit", "-q", "--no-verify", "-am", "change")
	for _, tc := range []struct {
		name, file, want string
	}{
		{"fresh file", fresh, "SetLatest (signature changed) and Options (unexported as options)."},
		{"file before the fields", old, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			tg, err := LoadTarget(dir, TargetOptions{Config: cfg, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
			if err != nil {
				t.Fatal(err)
			}
			c, failed, err := Check(t.Context(), tg, CheckOptions{BaselineFile: tc.file})
			if err != nil || len(failed) != 0 || c.Failed() {
				t.Fatalf("Check = (%v, %v), failed %v, want a passing check", failed, err, c != nil && c.Failed())
			}
			r := findRow(c, "p")
			if r == nil {
				t.Fatal("no row p")
			}
			got := reshapedHeadsUps(t, r)
			switch {
			case tc.want == "" && len(got) != 0:
				t.Errorf("heads-ups = %+v, want none", got)
			case tc.want != "" && (len(got) != 1 || !strings.Contains(got[0].Suggestion, tc.want)):
				t.Errorf("heads-ups = %+v, want one naming %s", got, tc.want)
			}
			if strings.Contains(logs.String(), "level=WARN") || strings.Contains(logs.String(), "level=ERROR") {
				t.Errorf("logs warn about the baseline file:\n%s", logs.String())
			}
		})
	}
}

// writeWithout copies the baseline file src to dst without the fields
// added for the reshaped-exports heads-up, the functions' signatures and
// the packages' exports, as a file written before them reads.
func writeWithout(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	var f map[string]any
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	stripped := 0
	for _, fns := range f["functions"].(map[string]any) {
		for _, fn := range fns.([]any) {
			if _, ok := fn.(map[string]any)["signature"]; ok {
				delete(fn.(map[string]any), "signature")
				stripped++
			}
		}
	}
	for _, row := range f["package_rows"].(map[string]any) {
		if _, ok := row.(map[string]any)["exports"]; ok {
			delete(row.(map[string]any), "exports")
			stripped++
		}
	}
	if stripped == 0 {
		t.Fatal("baseline file records no signatures or exports to strip")
	}
	out, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, out, 0o600); err != nil {
		t.Fatal(err)
	}
}
