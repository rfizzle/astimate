package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
)

// writeCapacityModule writes into dir a module whose packages a and b
// each export one function. With p set it also writes package p, which
// exports three functions and imports a and b.
func writeCapacityModule(t *testing.T, dir string, p bool) {
	t.Helper()
	files := map[string]string{
		"go.mod": "module example.com/capacity\n\ngo 1.24\n",
		"a/a.go": "package a\n\n// A returns one.\nfunc A() int { return 1 }\n",
		"b/b.go": "package b\n\n// B returns two.\nfunc B() int { return 2 }\n",
	}
	if p {
		files["p/p.go"] = "package p\n\nimport (\n\t\"example.com/capacity/a\"\n\t\"example.com/capacity/b\"\n)\n\n" +
			"// One returns a.\nfunc One() int { return a.A() }\n\n" +
			"// Two returns b.\nfunc Two() int { return b.B() }\n\n" +
			"// Three returns both.\nfunc Three() int { return a.A() + b.B() }\n"
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// capacityConfig is the embedded default with its thresholds replaced by
// exported_symbols at max 2 and internal_imports at max 1, both failing.
func capacityConfig(t *testing.T) string {
	t.Helper()
	head, _, ok := strings.Cut(string(config.Default()), "\nthresholds:\n")
	if !ok {
		t.Fatal("the embedded default has no thresholds section")
	}
	data := head + "\nthresholds:\n" +
		"  - metric: exported_symbols\n    kind: capacity\n    max: 2\n" +
		"  - metric: internal_imports\n    kind: capacity\n    max: 1\n"
	path := filepath.Join(t.TempDir(), "astimate.yaml")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestHookCapacityGuidance checks that the Stop hook's reason for a
// breach of exported_symbols or internal_imports tells the agent the real
// fix, names the edits that only lower the count as no fix, and says what
// to do when the split is out of scope: the workarounds an agent takes
// otherwise clear the number and leave no trace in the report.
func TestHookCapacityGuidance(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	dir := t.TempDir()
	writeCapacityModule(t, dir, false)
	base := filepath.Join(t.TempDir(), "baseline.json")
	var out, errOut bytes.Buffer
	if got := run([]string{"baseline", "write", dir, "--out", base}, &out, &errOut); got != exitOK {
		t.Fatalf("baseline write exit code = %d; stderr = %s", got, errOut.String())
	}
	writeCapacityModule(t, dir, true)

	out.Reset()
	errOut.Reset()
	args := []string{"check", dir, "--all", "--baseline", base, "--config", capacityConfig(t), "--format", "hook"}
	if got := run(args, &out, &errOut); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, errOut.String())
	}
	var block struct{ Decision, Reason string }
	if err := json.Unmarshal(out.Bytes(), &block); err != nil || block.Decision != "block" {
		t.Fatalf("hook stdout = %q, want a block decision (%v)", out.String(), err)
	}

	const outOfScope = "If the split is out of scope for this change, stop and say so instead of working around the limit, " +
		"so a person can record the debt as an exemption with a reason and an expiry."
	tests := []struct {
		metric, fix, notFix string
	}{
		{
			metric: "exported_symbols: 3 (no baseline), max 2. ",
			fix:    "The package exports 3 symbols: split it along one of its responsibilities.",
			notFix: "Not a fix: unexporting what callers need, folding a new function into an existing one's parameters " +
				"(a variadic or option argument), or changing what an existing export does.",
		},
		{
			metric: "internal_imports: 2 (no baseline), max 1. ",
			fix:    "The package imports 2 internal packages: move the code that needs them into a package of its own, or split it.",
			notFix: "Not a fix: moving code into a package it does not belong in, or injecting a dependency only to drop the import.",
		},
	}
	for _, tt := range tests {
		t.Run(strings.SplitN(tt.metric, ":", 2)[0], func(t *testing.T) {
			t.Parallel()

			var line string
			for l := range strings.SplitSeq(block.Reason, "\n") {
				if strings.HasPrefix(strings.TrimSpace(l), tt.metric) {
					line = strings.TrimSpace(l)
				}
			}
			want := tt.metric + tt.fix + " " + tt.notFix + " " + outOfScope
			if line != want {
				t.Errorf("finding line =\n%s\nwant\n%s\nreason:\n%s", line, want, block.Reason)
			}
		})
	}
}
