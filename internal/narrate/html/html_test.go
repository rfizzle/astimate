package html

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/narrate"
)

// fixtureReport is the module report of testdata/go/fixture, the golden
// of `astimate report --format json`.
const fixtureReport = "../../../cmd/astimate/testdata/report_fixture.json"

// readModule decodes a module report from path.
func readModule(t *testing.T, path string) *narrate.Module {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m narrate.Module
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return &m
}

// TestFixtureGolden checks the page for the fixture against
// testdata/report_fixture.html (regenerate with UPDATE_GOLDEN=1), with the
// fonts left out so the golden stays readable.
func TestFixtureGolden(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := render(&buf, readModule(t, fixtureReport), "/* fonts */"); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "report_fixture.html")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("reading golden file (regenerate with UPDATE_GOLDEN=1): %v", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("page differs from %s (regenerate with UPDATE_GOLDEN=1)", golden)
	}
}

// checkPage fails t when page loads anything, does not nest its tags
// properly, or lacks one of the sections tabs.
func checkPage(t *testing.T, page string, tabs []string) {
	t.Helper()
	for _, bad := range []string{"http://", "https://", "<link", " src=", "@import", "fetch(", "XMLHttpRequest"} {
		if strings.Contains(page, bad) {
			t.Errorf("page contains %q; it must load nothing", bad)
		}
	}
	// Leave out the style sheet and the script, whose text is not markup.
	body := regexp.MustCompile(`(?s)<style>.*?</style>|<script>.*?</script>`).ReplaceAllString(page, "")
	// A start or end tag: its slash, its name and whether it self-closes.
	tagRE := regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9]*)\b[^>]*?(/?)>`)
	voidElements := []string{"meta", "input", "br", "link", "img", "hr"}
	var stack []string
	for _, m := range tagRE.FindAllStringSubmatch(body, -1) {
		name := strings.ToLower(m[2])
		switch {
		case m[3] == "/" || slices.Contains(voidElements, name):
		case m[1] == "":
			stack = append(stack, name)
		case len(stack) == 0 || stack[len(stack)-1] != name:
			t.Fatalf("closing </%s> with open %q", name, stack)
		default:
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) != 0 {
		t.Errorf("tags left open: %q", stack)
	}
	for _, id := range tabs {
		if !strings.Contains(page, `data-tab="`+id+`"`) || !strings.Contains(page, `<section id="`+id+`"`) {
			t.Errorf("page has no %s tab and section", id)
		}
	}
	if !strings.HasPrefix(page, "<!DOCTYPE html>") {
		t.Error("page does not start with a doctype")
	}
}

func TestFixturePage(t *testing.T) {
	t.Parallel()

	m := readModule(t, fixtureReport)
	var buf bytes.Buffer
	if err := Write(&buf, m); err != nil {
		t.Fatal(err)
	}
	page := buf.String()
	checkPage(t, page, []string{"repo"})
	if strings.Contains(page, `data-tab="change"`) {
		t.Error("page without a change has a change tab")
	}
	for _, want := range []string{template2text(m.Headline), "font/woff2;base64,", `font-family: "Bricolage Grotesque"`,
		`font-family: "DM Sans"`, `font-family: "JetBrains Mono"`, "prefers-color-scheme: dark"} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if n := strings.Count(page, `<tr class="row"`); n != len(m.Packages) {
		t.Errorf("table has %d rows, want %d", n, len(m.Packages))
	}
	if n := strings.Count(page, `<circle r=`); n != len(m.Packages)+3 {
		t.Errorf("page has %d bubbles and ring arcs, want %d", n, len(m.Packages)+3)
	}
	if testing.Verbose() {
		t.Logf("fixture page: %d bytes", buf.Len())
	}
}

// template2text escapes s as html/template writes text.
func template2text(s string) string {
	return strings.NewReplacer("'", "&#39;", `"`, "&#34;").Replace(s)
}

func TestFormat(t *testing.T) {
	t.Parallel()

	for v, want := range map[float64]string{0: "0", 1234567: "1,234,567", 42.64: "42.6", 999.96: "1,000", -3.5: "-3.5", 0.04: "0"} {
		if got := fmtNum(v); got != want {
			t.Errorf("fmtNum(%v) = %q, want %q", v, got, want)
		}
	}
	for v, want := range map[float64]string{12.345: "12.35", -0.001: "0", 500: "500"} {
		if got := coord(v); got != want {
			t.Errorf("coord(%v) = %q, want %q", v, got, want)
		}
	}
	for p, want := range map[string]string{"internal/controller": "controller", "modules/x/y": "m/x/y",
		"internal/modules/z": "m/z", "cmd/app": "cmd/app"} {
		if got := shortPath(p); got != want {
			t.Errorf("shortPath(%q) = %q, want %q", p, got, want)
		}
	}
	if got := truncate("reconciliation", 5); got != "recon…" {
		t.Errorf("truncate = %q", got)
	}
}

func TestOpener(t *testing.T) {
	t.Parallel()

	tests := []struct {
		goos, name string
		args       []string
	}{
		{"linux", "xdg-open", []string{"/r.html"}},
		{"freebsd", "xdg-open", []string{"/r.html"}},
		{"darwin", "open", []string{"/r.html"}},
		{"windows", "rundll32", []string{"url.dll,FileProtocolHandler", "/r.html"}},
	}
	for _, tt := range tests {
		if name, args := opener(tt.goos, "/r.html"); name != tt.name || !slices.Equal(args, tt.args) {
			t.Errorf("opener(%s) = %s %q, want %s %q", tt.goos, name, args, tt.name, tt.args)
		}
	}
}

// TestEmptyModule checks a module without packages renders, with no ring
// arcs, which an all-zero dash would draw as full circles.
func TestEmptyModule(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := render(&buf, &narrate.Module{Name: "Empty"}, ""); err != nil {
		t.Fatal(err)
	}
	checkPage(t, buf.String(), []string{"repo"})
	if strings.Contains(buf.String(), "stroke-dasharray=\"0 0\"") {
		t.Error("empty module draws ring arcs")
	}
}

func TestFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args []string
		want Options
	}{
		{nil, Options{Out: DefaultOut}},
		{[]string{"--out", "r.html", "--open"}, Options{Out: "r.html", Open: true}},
	}
	for _, tt := range tests {
		fs := flag.NewFlagSet("report", flag.ContinueOnError)
		o := Flags(fs)
		if err := fs.Parse(tt.args); err != nil || *o != tt.want {
			t.Errorf("Flags after %q = %+v, %v; want %+v", tt.args, *o, err, tt.want)
		}
	}
}

func TestSave(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "page.html")
	var stdout bytes.Buffer
	if err := Save(t.Context(), readModule(t, fixtureReport), Options{Out: out}, &stdout); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != out+"\n" {
		t.Errorf("stdout = %q, want the path written", stdout.String())
	}
	page, err := os.ReadFile(out)
	if err != nil || !bytes.HasPrefix(page, []byte("<!DOCTYPE html>")) {
		t.Errorf("reading the page: %v", err)
	}
	if err := Save(t.Context(), readModule(t, fixtureReport), Options{Out: filepath.Join(out, "x.html")}, &stdout); err == nil {
		t.Error("Save under a file succeeded, want an error")
	}
}
