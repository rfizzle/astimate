package action_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// readRepoFile returns the contents of path, relative to the repository root
// one directory above this package.
func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// makefileTags returns the words of the Makefile's BUILD_TAGS assignment.
func makefileTags(t *testing.T, src string) []string {
	t.Helper()
	for line := range strings.Lines(src) {
		if rest, ok := strings.CutPrefix(line, "BUILD_TAGS :="); ok {
			return strings.Fields(rest)
		}
	}
	t.Fatal("Makefile: no BUILD_TAGS := line")
	return nil
}

// goreleaserTags returns the items of the first `tags:` list in
// .goreleaser.yaml, read as the `- item` lines that follow the key.
func goreleaserTags(t *testing.T, src string) []string {
	t.Helper()
	var tags []string
	in := false
	for line := range strings.Lines(src) {
		trimmed := strings.TrimSpace(line)
		if !in {
			in = trimmed == "tags:"
			continue
		}
		item, ok := strings.CutPrefix(trimmed, "- ")
		if !ok {
			break
		}
		tags = append(tags, strings.TrimSpace(item))
	}
	if len(tags) == 0 {
		t.Fatal(".goreleaser.yaml: no tags: list")
	}
	return tags
}

// installTags returns the words of the single-quoted -tags argument in
// install.sh.
func installTags(t *testing.T, src string) []string {
	t.Helper()
	_, rest, ok := strings.Cut(src, "-tags '")
	if !ok {
		t.Fatal("install.sh: no -tags '...' argument")
	}
	list, _, ok := strings.Cut(rest, "'")
	if !ok {
		t.Fatal("install.sh: unterminated -tags argument")
	}
	return strings.Fields(list)
}

// TestBuildTagsAgree fails when the grammar build tags in .goreleaser.yaml or
// install.sh drift from BUILD_TAGS in the Makefile. A misspelled tag still
// compiles but silently leaves a grammar out, so the copies are compared here.
func TestBuildTagsAgree(t *testing.T) {
	want := makefileTags(t, readRepoFile(t, "Makefile"))
	if len(want) == 0 {
		t.Fatal("Makefile: BUILD_TAGS is empty")
	}
	tests := []struct {
		name string
		got  []string
	}{
		{".goreleaser.yaml", goreleaserTags(t, readRepoFile(t, ".goreleaser.yaml"))},
		{"action/install.sh", installTags(t, readRepoFile(t, "action/install.sh"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !slices.Equal(tt.got, want) {
				t.Errorf("tags = %q, want BUILD_TAGS from the Makefile %q", tt.got, want)
			}
		})
	}
}
