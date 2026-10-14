package summary

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// summaryRows is a replay output of five commits: one failed by sloc
// alone, one failed by dup_blocks (merged with duplication_pct) in a and
// sloc in b, one passed, one touching no package, and one not loaded.
func summaryRows(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	commits := `{"commit":"c1","subject":"one","loaded":true,"packages_changed":["a"],"packages_deleted":[],"passed":false}
{"commit":"c2","subject":"two","loaded":true,"packages_changed":["a","b"],"packages_deleted":[],"passed":false}
{"commit":"c3","subject":"three","loaded":true,"packages_changed":["b"],"packages_deleted":[],"passed":true}
{"commit":"c4","subject":"four","loaded":true,"packages_changed":[],"packages_deleted":[],"passed":true}
{"commit":"c5","subject":"five","loaded":false,"error":"no go.mod found","packages_changed":[],"packages_deleted":[],"passed":null}
`
	packages := `{"commit":"c1","package":"a","violations":[{"metric":"sloc"}]}
{"commit":"c2","package":"<module>","violations":[]}
{"commit":"c2","package":"a","violations":[{"metric":"dup_blocks","also":["duplication_pct"]}]}
{"commit":"c2","package":"b","violations":[{"metric":"sloc"}]}
{"commit":"c3","package":"b","violations":[]}
`
	for name, data := range map[string]string{commitsFile: commits, packagesFile: packages} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestSummarize(t *testing.T) {
	t.Parallel()

	s, err := Summarize(summaryRows(t))
	if err != nil {
		t.Fatal(err)
	}
	want := &Summary{
		Commits:   5,
		NotLoaded: []NotLoaded{{Commit: "c5", Subject: "five", Error: "no go.mod found"}},
		Checked:   4,
		Touched:   3,
		Failed:    2,
		Rules: []RuleCount{
			{Metric: "sloc", Failed: 2, Alone: 1},
			{Metric: "dup_blocks", Failed: 1},
			{Metric: "duplication_pct", Failed: 1},
		},
		Packages: []PackageCount{{Package: "a", Failed: 2}, {Package: "b", Failed: 1}},
	}
	s.merged = nil
	if !reflect.DeepEqual(s, want) {
		t.Errorf("summary = %+v\nwant      %+v", s, want)
	}
	// sloc failed 2 of the 4 commits checked, the others 1.
	if got := s.OverBudget(0.4); !slices.Equal(got, []string{"sloc"}) {
		t.Errorf("over a 40%% budget: %v, want [sloc]", got)
	}
	if got := s.OverBudget(0.25); !slices.Equal(got, []string{"sloc"}) {
		t.Errorf("over a budget equal to a rule's share: %v, want only the rule above it", got)
	}
	if s.Share(s.Failed) != 0.5 || s.TouchedShare(s.Failed) != 2.0/3 {
		t.Errorf("failed share %v of checked, %v of touched; want 0.5 and 2/3", s.Share(s.Failed), s.TouchedShare(s.Failed))
	}
	if got := s.OverBudget(0.1); !slices.Equal(got, []string{"dup_blocks", "duplication_pct", "sloc"}) {
		t.Errorf("over a 10%% budget: %v", got)
	}
}

// TestWarn checks that a rule whose every violation was merged into by a
// rule over budget warns with it, and that one with a violation of its own
// or merged with a rule under budget keeps its severity.
func TestWarn(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	var commits, packages strings.Builder
	// Ten commits checked: tokens_est alone fails c1 to c4; c5 and c7 fail
	// api_surface merged with tokens_est; c6 fails dup_blocks merged with
	// duplication_pct; c7 also fails sloc alone; c8 to c10 pass.
	rows := map[string]string{
		"c1": `{"metric":"tokens_est"}`, "c2": `{"metric":"tokens_est"}`,
		"c3": `{"metric":"tokens_est"}`, "c4": `{"metric":"tokens_est"}`,
		"c5": `{"metric":"api_surface","also":["tokens_est"]}`,
		"c6": `{"metric":"dup_blocks","also":["duplication_pct"]}`,
		"c7": `{"metric":"api_surface","also":["tokens_est"]},{"metric":"sloc"}`,
	}
	for i := 1; i <= 10; i++ {
		hash := "c" + strconv.Itoa(i)
		viol := rows[hash]
		fmt.Fprintf(&commits, `{"commit":%q,"loaded":true,"packages_changed":["a"],"packages_deleted":[],"passed":%v}`+"\n", hash, viol == "")
		fmt.Fprintf(&packages, `{"commit":%q,"package":"a","violations":[%s]}`+"\n", hash, viol)
	}
	for name, data := range map[string]string{commitsFile: commits.String(), packagesFile: packages.String()} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Summarize(dir)
	if err != nil {
		t.Fatal(err)
	}
	// tokens_est failed 6 of 10, api_surface 2, the rest 1.
	for _, tt := range []struct {
		budget float64
		want   []string
	}{
		// Only tokens_est is over, and every violation of api_surface is
		// merged into by it; sloc has a violation of its own and dup_blocks
		// is merged into by a rule under budget.
		{0.5, []string{"api_surface", "tokens_est"}},
		{0.1, []string{"api_surface", "tokens_est"}},
		{0.05, []string{"api_surface", "dup_blocks", "duplication_pct", "sloc", "tokens_est"}},
		{0.9, nil},
	} {
		if got := s.Warn(tt.budget); !slices.Equal(got, tt.want) {
			t.Errorf("Warn(%v) = %v, want %v", tt.budget, got, tt.want)
		}
	}
}

// TestWriteRows checks the rows are written per commit, its package rows
// before its commit row, in range order.
func TestWriteRows(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WriteRows(&buf, summaryRows(t)); err != nil {
		t.Fatal(err)
	}
	var got []string
	for line := range bytes.Lines(buf.Bytes()) {
		hash, err := commitOf(line)
		if err != nil {
			t.Fatal(err)
		}
		kind := "commit"
		if bytes.Contains(line, []byte(`"package":`)) {
			kind = "package"
		}
		got = append(got, hash+" "+kind)
	}
	want := []string{"c1 package", "c1 commit", "c2 package", "c2 package", "c2 package", "c2 commit",
		"c3 package", "c3 commit", "c4 commit", "c5 commit"}
	if !slices.Equal(got, want) {
		t.Errorf("rows %q\nwant %q", got, want)
	}
}
