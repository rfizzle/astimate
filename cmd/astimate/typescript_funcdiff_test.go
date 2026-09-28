package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// tsGrade is one complex TypeScript function, the counterpart of the Go
// degraded fixture's grade: cognitive complexity 60 with nesting 2, so it
// fails changed_func_cognitive_max (max 55 in the default typescript
// override) while the package's p90 stays at the level of its small
// functions.
const tsGrade = `
// grade rates a passphrase from 0 to 10.
function grade(s: string, strict: boolean): number {
  let upper = 0, lower = 0, digit = 0, other = 0;
  for (const c of s) {
    switch (true) {
      case /[A-Z]/.test(c):
        upper++;
        break;
      case /[a-z]/.test(c):
        lower++;
        break;
      case /[0-9]/.test(c):
        digit++;
        break;
      default:
        other++;
    }
  }
  let score = 0;
  if (s.length >= 12 && upper > 0 || lower > 3 && !strict || digit > 6) {
    score += 2;
  }
  if (digit > 1 || other > 0 && s.length > 8 || strict && upper > 2 || s.length > 16) {
    score++;
  }
  if (upper === 0 && lower === 0 || s.length < 4 && !strict) {
    return 0;
  }
  if (strict && (digit === 0 || other === 0) && s.length < 16 || upper > 12) {
    score -= 3;
  } else if (!strict && digit + other > 4 || upper > 5 && lower > 5) {
    score += 3;
  } else {
    score--;
  }
  for (let i = 1; i < s.length; i++) {
    if (s[i] === s[i - 1] && /[a-zA-Z]/.test(s[i]) || s[i] === " " && strict) {
      score--;
    }
  }
  if (upper > lower || digit > upper + lower && !strict || other > 5) {
    score -= 2;
  }
  if (other > 3 && digit > 3 || upper > 3 && lower > 3 || s.length > 24) {
    score += 2;
  }
  if (strict && s.length > 20 || !strict && other === 0 || digit > 9) {
    score--;
  }
  if (digit > 0 && other > 0 && upper > 0 && lower > 0 || s.length > 30 && strict) {
    score += 2;
  }
  if (upper > 1 && lower > 1 || digit > 1 && other > 1 || s.length > 40) {
    score++;
  }
  if (!strict && upper > 7 || strict && lower > 7) {
    score--;
  }
  return Math.max(0, Math.min(score, 10));
}
`

// tsGradeLine returns the start of the changed_func_cognitive_max
// suggestion naming grade, with the line grade is declared on once tsGrade
// is appended to tested/tested.ts.
func tsGradeLine(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(tsFixtureDir, "tested", "tested.ts"))
	if err != nil {
		t.Fatal(err)
	}
	line := bytes.Count(src, []byte{'\n'}) + 3
	return "Changed function grade (tested.ts:" + strconv.Itoa(line) + ") has cognitive complexity 60"
}

// TestTypeScriptChangedFunction runs check without --all on a TypeScript
// repository against master: a complex function added to a package fails
// changed_func_cognitive_max and is named in the suggestion, a comment and
// layout edit to a complex function already on master changes no function,
// and an operator edit to it marks it changed again.
func TestTypeScriptChangedFunction(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Parallel()

	file := func(repo string) string { return filepath.Join(repo, "tested", "tested.ts") }
	// commitGrade adds grade to the fixture on master, so it is in the
	// baseline.
	commitGrade := func(t *testing.T, repo string) {
		t.Helper()
		appendFile(t, file(repo), tsGrade)
		gitIn(t, repo, "commit", "-q", "--no-verify", "-am", "add grade")
	}
	// rewrite replaces old with repl in tested.ts.
	rewrite := func(t *testing.T, repo, old, repl string) {
		t.Helper()
		src, err := os.ReadFile(file(repo))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(src, []byte(old)) {
			t.Fatalf("tested.ts does not contain %q", old)
		}
		if err := os.WriteFile(file(repo), bytes.Replace(src, []byte(old), []byte(repl), 1), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name     string
		change   func(t *testing.T, repo string)
		wantExit int
		// wantMax is the expected changed_func_cognitive_max of tested.
		wantMax int
	}{
		{
			name:     "complex function added",
			change:   func(t *testing.T, repo string) { appendFile(t, file(repo), tsGrade) },
			wantExit: exitGateFailed,
			wantMax:  60,
		},
		{
			name: "comment and layout edit",
			change: func(t *testing.T, repo string) {
				commitGrade(t, repo)
				rewrite(t, repo, "  let score = 0;\n", "  // start from nothing\n  let score = 0\n\n")
				rewrite(t, repo, "    score += 3;\n", "    score += 3; /* bonus */\n")
			},
			wantExit: exitOK,
			wantMax:  0,
		},
		{
			name: "operator edit",
			change: func(t *testing.T, repo string) {
				commitGrade(t, repo)
				rewrite(t, repo, "score -= 3;", "score += 3;")
			},
			wantExit: exitGateFailed,
			wantMax:  60,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo := newTSFixtureRepo(t)
			tt.change(t, repo)

			var stdout, stderr bytes.Buffer
			args := []string{"check", repo, "--format", formatJSON}
			if got := run(args, &stdout, &stderr); got != tt.wantExit {
				t.Fatalf("run(%q) exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s",
					args, got, tt.wantExit, stdout.String(), stderr.String())
			}
			reports := decodeReports(t, stdout.Bytes())
			var found bool
			for i := range reports {
				r := &reports[i]
				if r.PackagePath != "tested" {
					continue
				}
				found = true
				got := r.Metrics.ChangedFuncCognitiveMax
				if got == nil || *got != tt.wantMax {
					t.Errorf("tested changed_func_cognitive_max = %v, want %d", got, tt.wantMax)
				}
				var named bool
				for _, v := range r.Violations {
					if v.Metric == "changed_func_cognitive_max" {
						named = strings.HasPrefix(v.Suggestion, tsGradeLine(t))
						if !named {
							t.Errorf("suggestion = %q, want it to start %q", v.Suggestion, tsGradeLine(t))
						}
					}
				}
				if want := tt.wantExit == exitGateFailed; named != want {
					t.Errorf("changed_func_cognitive_max violation naming grade = %v, want %v", named, want)
				}
			}
			if !found {
				t.Fatalf("no report for tested in %s", stdout.String())
			}
		})
	}
}
