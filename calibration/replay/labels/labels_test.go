package labels

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hashC = "cccccccccccccccccccccccccccccccccccccccc"
)

// synthetic is a valid labels file for hashA and hashB, one hand label and
// one rule label.
const synthetic = `source:
  repository: https://example.com/fix.git
  range: v1..v2
  data: calibration/data/replay-fix-2026-01-01
rule: reverted later
commits:
  - hash: ` + hashA + `
    verdict: allow
    reason: small, tested change
    provenance: confirmed
  - hash: ` + hashB + `
    verdict: block
    reason: reverted by ` + hashC + `
    metrics: [dup_blocks]
    provenance: rule
    agent: true
    fired:
      - part: revert
        by: ` + hashC + `
`

func writeSynthetic(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fix.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name     string
		edit     func(string) string
		replayed []string
		want     string // substring of the error; empty for valid
	}{
		{"valid", func(s string) string { return s }, []string{hashA, hashB}, ""},
		{"missing commit", func(s string) string { return s }, []string{hashA, hashB, hashC}, hashC + " has no label"},
		{"not replayed", func(s string) string { return s }, []string{hashB}, "was not replayed"},
		{"duplicate", func(s string) string { return strings.Replace(s, hashB+"\n    verdict", hashA+"\n    verdict", 1) },
			[]string{hashA}, hashA + " is labeled twice"},
		{"bad verdict", func(s string) string { return strings.Replace(s, "verdict: allow", "verdict: maybe", 1) },
			[]string{hashA, hashB}, `verdict "maybe"`},
		{"no reason", func(s string) string { return strings.Replace(s, "reason: small, tested change", `reason: ""`, 1) },
			[]string{hashA, hashB}, "no reason"},
		{"bad provenance", func(s string) string { return strings.Replace(s, "provenance: confirmed", "provenance: guessed", 1) },
			[]string{hashA, hashB}, `provenance "guessed"`},
		{"evidence on allow", func(s string) string { return strings.Replace(s, "verdict: block", "verdict: allow", 1) },
			[]string{hashA, hashB}, "rule evidence on a allow label"},
		{"bad part", func(s string) string { return strings.Replace(s, "part: revert", "part: hunch", 1) },
			[]string{hashA, hashB}, `evidence "hunch"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, err := Load(writeSynthetic(t, tc.edit(synthetic)))
			if err != nil {
				t.Fatal(err)
			}
			err = f.Validate(tc.replayed)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("Validate = %v, want nil", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("Validate = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	_, err := Load(writeSynthetic(t, strings.Replace(synthetic, "verdict: allow", "verdict: allow\n    verdit: allow", 1)))
	if err == nil || !strings.Contains(err.Error(), "verdit") {
		t.Fatalf("Load = %v, want an unknown-field error", err)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("Load of a missing file succeeded")
	}
}

func TestDecode(t *testing.T) {
	var v struct {
		Name string `yaml:"name"`
	}
	if err := Decode(writeSynthetic(t, "name: a\n"), &v); err != nil || v.Name != "a" {
		t.Fatalf("Decode = %v, %+v", err, v)
	}
	if err := Decode(writeSynthetic(t, "name: a\nnmae: b\n"), &v); err == nil || !strings.Contains(err.Error(), "nmae") {
		t.Fatalf("Decode of an unknown key = %v", err)
	}
}

func TestWriteRoundTrip(t *testing.T) {
	f, err := Load(writeSynthetic(t, synthetic))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "out.yaml")
	if err := Write(path, f); err != nil {
		t.Fatal(err)
	}
	g, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Validate([]string{hashA, hashB}); err != nil {
		t.Fatal(err)
	}
	byHash := g.ByHash()
	b := byHash[hashB]
	if b == nil || b.Verdict != Block || b.Agent == nil || !*b.Agent || len(b.Fired) != 1 || b.Fired[0].By != hashC ||
		len(b.Metrics) != 1 || byHash[hashA].Agent != nil {
		t.Fatalf("round trip lost fields: %+v", b)
	}
	if err := Write(filepath.Join(t.TempDir(), "no", "such", "dir.yaml"), f); err == nil {
		t.Fatal("Write into a missing directory succeeded")
	}
}

func TestReplayed(t *testing.T) {
	dir := t.TempDir()
	rows := `{"commit":"` + hashA + `","subject":"a"}` + "\n" + `{"commit":"` + hashB + `","subject":"b"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "commits.jsonl"), []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Replayed(dir)
	if err != nil || len(got) != 2 || got[0] != hashA || got[1] != hashB {
		t.Fatalf("Replayed = %v, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "commits.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Replayed(dir); err == nil {
		t.Fatal("Replayed accepted a row without a commit")
	}
	if _, err := Replayed(t.TempDir()); err == nil {
		t.Fatal("Replayed of a directory without commits.jsonl succeeded")
	}
}
