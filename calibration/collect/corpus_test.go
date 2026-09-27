package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// TestCorpusFile checks that the shipped corpus parses and satisfies the
// schema: at least minExternalModules cloned modules plus the standard
// library, each with the required fields.
func TestCorpusFile(t *testing.T) {
	c, err := LoadCorpus("../corpus.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var external, local int
	for _, e := range c.Modules {
		if e.Local {
			local++
			if e.Module != stdlibModule {
				t.Errorf("local entry %s, want %s", e.Module, stdlibModule)
			}
			continue
		}
		external++
		if e.Repo == "" || e.License == "" || e.Reason == "" || e.StarsOrDependents == "" {
			t.Errorf("%s: missing a required field: %+v", e.Module, e)
		}
	}
	if local != 1 || external < minExternalModules {
		t.Errorf("got %d local and %d cloned modules, want 1 and at least %d", local, external, minExternalModules)
	}

	// Every unpinned entry must be in the form --pin can fill.
	data, err := os.ReadFile("../corpus.yaml")
	if err != nil {
		t.Fatal(err)
	}
	fake := func(context.Context, string) (string, error) { return strings.Repeat("a", 40), nil }
	if _, _, err := PinCorpus(t.Context(), data, fake); err != nil {
		t.Errorf("pinning the corpus: %v", err)
	}
}

// validCorpus returns a corpus document with the standard library and n
// cloned modules, each unpinned.
func validCorpus(n int) string {
	var b strings.Builder
	b.WriteString("note: test\nmodules:\n")
	b.WriteString("  - module: std\n    local: true\n    license: BSD-3-Clause\n    stars_or_dependents: all\n    reason: r\n")
	for i := range n {
		b.WriteString("  - module: example.com/m")
		b.WriteByte(byte('a' + i))
		b.WriteString("\n    repo: https://example.com/m.git\n    commit: \"\"\n    license: MIT\n")
		b.WriteString("    stars_or_dependents: many\n    reason: r\n")
	}
	return b.String()
}

func TestParseCorpus(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	tests := []struct {
		name    string
		doc     string
		wantErr string
	}{
		{"valid", validCorpus(minExternalModules), ""},
		{"too few modules", validCorpus(minExternalModules - 1), "at least 20"},
		{"empty", "", "empty document"},
		{"unknown key", validCorpus(minExternalModules) + "extra: 1\n", "field extra not found"},
		{"no stdlib", strings.Replace(validCorpus(minExternalModules), "local: true", "repo: x", 1), "exactly one local"},
		{"copyleft license", strings.Replace(validCorpus(minExternalModules), "license: MIT", "license: AGPL-3.0", 1), "AGPL-3.0"},
		{"missing reason", strings.Replace(validCorpus(minExternalModules), "reason: r\n  - module: example.com/ma", "reason: \"\"\n  - module: example.com/ma", 1), "reason is required"},
		{"short commit", strings.Replace(validCorpus(minExternalModules), `commit: ""`, "commit: abc123", 1), "40-character"},
		{"full commit", strings.Replace(validCorpus(minExternalModules), `commit: ""`, "commit: "+sha, 1), ""},
		{"duplicate", validCorpus(minExternalModules) + "  - module: example.com/ma\n    repo: r\n    license: MIT\n    stars_or_dependents: s\n    reason: r\n", "duplicate module"},
		{"stdlib with commit", strings.Replace(validCorpus(minExternalModules), "local: true", "local: true\n    commit: "+sha, 1), "no repo or commit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCorpus([]byte(tt.doc))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestPinCorpus(t *testing.T) {
	const pinned = "1111111111111111111111111111111111111111"
	doc := "# keep this comment\n" + strings.Replace(validCorpus(minExternalModules), `commit: ""`, "commit: "+pinned, 1)
	var calls int
	resolve := func(_ context.Context, repo string) (string, error) {
		calls++
		if repo != "https://example.com/m.git" {
			t.Errorf("resolve(%q)", repo)
		}
		return "abcdefabcdefabcdefabcdefabcdefabcdefabcd", nil
	}
	out, n, err := PinCorpus(t.Context(), []byte(doc), resolve)
	if err != nil {
		t.Fatal(err)
	}
	if n != minExternalModules-1 || calls != n {
		t.Errorf("pinned %d with %d calls, want %d", n, calls, minExternalModules-1)
	}
	c, err := ParseCorpus(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range c.Modules {
		if !e.Local && !isCommitHash(e.Commit) {
			t.Errorf("%s left unpinned: %q", e.Module, e.Commit)
		}
	}
	if c.Modules[1].Commit != pinned {
		t.Errorf("existing pin replaced: %q", c.Modules[1].Commit)
	}
	want := strings.ReplaceAll(doc, `commit: ""`, `commit: "abcdefabcdefabcdefabcdefabcdefabcdefabcd"`)
	if string(out) != want {
		t.Errorf("pin changed more than the commits:\n%s", out)
	}

	t.Run("a failed resolve pins nothing", func(t *testing.T) {
		boom := errors.New("boom")
		_, _, err := PinCorpus(t.Context(), []byte(doc), func(context.Context, string) (string, error) { return "", boom })
		if !errors.Is(err, boom) {
			t.Errorf("err = %v, want boom", err)
		}
	})
}

func TestSelectEntries(t *testing.T) {
	c, err := ParseCorpus([]byte(validCorpus(minExternalModules)))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := selectEntries(c, stdlibModule); err != nil || len(got) != 1 || !got[0].Local {
		t.Errorf("stdlib only = %v, %v", got, err)
	}
	if _, err := selectEntries(c, ""); err == nil || !strings.Contains(err.Error(), "--pin") {
		t.Errorf("unpinned full run err = %v, want one naming --pin", err)
	}
	if _, err := selectEntries(c, "example.com/nope"); err == nil {
		t.Error("unknown module selected")
	}
}
