package action_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// existingComments is the API's pretty-printed listing of a pull request's
// comments: another person's, then astimate's, which carries the marker.
const existingComments = `[
  {
    "url": "https://api.github.com/repos/o/r/issues/comments/11",
    "id": 11,
    "user": {
      "login": "someone",
      "id": 900
    },
    "body": "looks good, <!-- not ours -->"
  },
  {
    "url": "https://api.github.com/repos/o/r/issues/comments/42",
    "id": 42,
    "user": {
      "login": "github-actions[bot]",
      "id": 41898282
    },
    "body": "<!-- astimate-report -->\n## astimate: PASS\n"
  }
]
`

// TestHTML runs html.sh with a fake astimate printing its arguments: the
// report is written for the same base and configuration as the check, and
// a failure only warns.
func TestHTML(t *testing.T) {
	bin := t.TempDir()
	fake := "#!/bin/sh\necho \"args $*\"\nexit \"${FAKE_EXIT:-0}\"\n"
	if err := os.WriteFile(filepath.Join(bin, "astimate"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	path := "PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")
	code, stdout, stderr := runScript(t, "html.sh", []string{path, "ASTIMATE_BASE=origin/master", "ASTIMATE_CONFIG=a.yaml", "ASTIMATE_PATH=svc"})
	if want := "args report --format html --out astimate-report.html --config a.yaml --base origin/master -- svc\n"; code != 0 || stdout != want {
		t.Errorf("exit %d, stdout %q, want 0 and %q\nstderr: %s", code, stdout, want, stderr)
	}
	code, _, stderr = runScript(t, "html.sh", []string{path, "FAKE_EXIT=2"})
	if code != 0 || !strings.Contains(stderr, "the HTML report was not written") {
		t.Errorf("exit %d, stderr %q, want 0 and a warning", code, stderr)
	}
}

// TestComment runs comment.sh in its dry-run mode, with a fake astimate on
// PATH printing a Markdown report: the request body is the marker and the
// report as one JSON string, the first run posts, and the next, which finds
// the marker in the listing, updates that comment, so it is never posted
// twice.
func TestComment(t *testing.T) {
	bin := t.TempDir()
	fake := "#!/bin/sh\necho \"## astimate: FAIL — \\\"a\\\" | b\"\necho \"| \\`p\\` | x\\\\y |\"\nexit \"${FAKE_EXIT:-3}\"\n"
	if err := os.WriteFile(filepath.Join(bin, "astimate"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	listing := filepath.Join(t.TempDir(), "comments.json")
	if err := os.WriteFile(listing, []byte(existingComments), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(t.TempDir(), "none.json")
	if err := os.WriteFile(empty, []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := []string{"ASTIMATE_COMMENT_DRY_RUN=1", "ASTIMATE_PR_NUMBER=7", "GITHUB_REPOSITORY=o/r", "ASTIMATE_BASE=",
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")}
	body := `{"body":"<!-- astimate-report -->\n## astimate: FAIL — \"a\" | b\n| ` + "`p`" + ` | x\\y |\n"}`
	tests := []struct {
		name       string
		env        []string
		wantStdout string
		wantStderr string
	}{
		{name: "first run posts", env: []string{"ASTIMATE_COMMENTS_FILE=" + empty},
			wantStdout: "POST https://api.github.com/repos/o/r/issues/7/comments\n" + body + "\n"},
		{name: "next run updates in place", env: []string{"ASTIMATE_COMMENTS_FILE=" + listing},
			wantStdout: "PATCH https://api.github.com/repos/o/r/issues/comments/42\n" + body + "\n"},
		{name: "artifact pointer", env: []string{"ASTIMATE_COMMENTS_FILE=" + empty, "ASTIMATE_HTML=true"},
			wantStdout: "POST https://api.github.com/repos/o/r/issues/7/comments\n" + strings.TrimSuffix(body, `\n"}`) +
				"\\nFull report: `astimate-report.html` in the `astimate-report` artifact of this run.\\n\"}\n"},
		{name: "enterprise api url", env: []string{"ASTIMATE_COMMENTS_FILE=" + empty, "GITHUB_API_URL=https://ghe.example/api/v3"},
			wantStdout: "POST https://ghe.example/api/v3/repos/o/r/issues/7/comments\n" + body + "\n"},
		{name: "analysis failed", env: []string{"FAKE_EXIT=2"}, wantStderr: "astimate check exited 2; no comment posted"},
		{name: "no pull request", env: []string{"ASTIMATE_PR_NUMBER="}, wantStderr: "no pull request number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runScript(t, "comment.sh", append(append([]string{}, base...), tt.env...))
			if code != 0 {
				t.Errorf("exit = %d, want 0: a comment problem never fails the job\nstderr: %s", code, stderr)
			}
			if stdout != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout, tt.wantStdout)
			}
			if !strings.Contains(stderr, tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tt.wantStderr)
			}
		})
	}
	t.Run("no token", func(t *testing.T) {
		env := append(append([]string{}, base...), "ASTIMATE_COMMENT_DRY_RUN=", "GITHUB_TOKEN=")
		code, stdout, stderr := runScript(t, "comment.sh", env)
		if code != 0 || stdout != "" || !strings.Contains(stderr, "no GITHUB_TOKEN") {
			t.Errorf("exit %d, stdout %q, stderr %q, want a warning and nothing sent", code, stdout, stderr)
		}
	})
}
