package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

// rolesModule writes a module of twelve leaf packages p01 to p12, a
// package main cmd/app and a library wire that both import all twelve,
// and a baseline file of the module without cmd/app and wire, so both are
// new at head; it returns the module root and the baseline file.
func rolesModule(t *testing.T) (root, base string) {
	t.Helper()
	root = t.TempDir()
	write := func(rel, data string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/roles\n\ngo 1.27\n")
	var imports strings.Builder
	for i := 1; i <= 12; i++ {
		name := fmt.Sprintf("p%02d", i)
		write(name+"/p.go", "package "+name+"\n\n// N is a constant.\nconst N = 1\n")
		imports.WriteString("\t_ \"example.com/roles/" + name + "\"\n")
	}
	write("cmd/app/main.go", "package main\n\nimport (\n"+imports.String()+")\n\nfunc main() {}\n")
	write("wire/wire.go", "// Package wire imports what cmd/app imports.\npackage wire\n\nimport (\n"+imports.String()+")\n")
	base = filepath.Join(t.TempDir(), "baseline.json")
	if err := baseline.Write(base, "", "example.com/roles", "est", map[string]metrics.RawMetrics{}); err != nil {
		t.Fatal(err)
	}
	return root, base
}

// TestCheckRoleInternalImports checks the default's internal_imports
// rule judges library packages only (SPEC.md 8.2): twelve internal
// imports pass in a package main and fail the same imports in a library,
// and the JSON reports and rank rows carry each package's role.
func TestCheckRoleInternalImports(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	root, base := rolesModule(t)
	var out, errOut bytes.Buffer
	args := []string{"check", root, "--all", "--baseline", base, "--format", formatJSON}
	if code := run(args, &out, &errOut); code != exitGateFailed {
		t.Fatalf("run(%q) exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", args, code, exitGateFailed, out.String(), errOut.String())
	}
	reports := decodeReports(t, out.Bytes())
	byPath := map[string]*report.Report{}
	for i := range reports {
		byPath[reports[i].PackagePath] = &reports[i]
	}
	for _, c := range []struct {
		path, role string
		fails      bool
	}{
		{"cmd/app", "main", false},
		{"wire", "library", true},
		{"p01", "library", false},
	} {
		r := byPath[c.path]
		if r == nil {
			t.Fatalf("no report for %s in %s", c.path, out.String())
		}
		if r.Role != c.role {
			t.Errorf("%s role = %q, want %q", c.path, r.Role, c.role)
		}
		got := false
		for _, v := range r.Violations {
			got = got || v.Metric == "internal_imports"
		}
		if got != c.fails {
			t.Errorf("%s internal_imports violation = %v, want %v; violations %+v", c.path, got, c.fails, r.Violations)
		}
	}
	if m := byPath["cmd/app"].Metrics.InternalImports; m != 12 {
		t.Errorf("cmd/app internal_imports = %d, want 12", m)
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"rank", root, "--json"}, &out, &errOut); code != exitOK {
		t.Fatalf("rank exit code = %d, stderr = %q", code, errOut.String())
	}
	var rows []report.Row
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	roles := map[string]string{}
	for _, r := range rows {
		roles[r.Path] = r.Role
	}
	if roles["cmd/app"] != "main" || roles["wire"] != "library" {
		t.Errorf("rank roles = %v, want cmd/app main and wire library", roles)
	}
}
