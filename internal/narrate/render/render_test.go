package render

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/narrate"
)

func TestWrite(t *testing.T) {
	t.Parallel()

	m := &narrate.Module{Name: "Demo", Headline: "Demo has no package too big to rebuild in one go."}
	for _, format := range Formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			opts := &Options{Width: 80, Out: filepath.Join(t.TempDir(), "page.html")}
			if err := Write(t.Context(), &buf, m, format, opts); err != nil || buf.Len() == 0 {
				t.Fatalf("Write(%s) = %v with %d bytes, want output", format, err, buf.Len())
			}
		})
	}
	var buf bytes.Buffer
	if err := Write(t.Context(), &buf, m, FormatJSON, &Options{}); err != nil {
		t.Fatal(err)
	}
	var got narrate.Module
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil || got.Headline != m.Headline {
		t.Errorf("json = %s (%v), want the module", buf.String(), err)
	}
	if err := Write(t.Context(), &buf, m, "xml", &Options{}); err == nil || !strings.Contains(err.Error(), `"xml"`) {
		t.Errorf("Write(xml) = %v, want an unknown format error", err)
	}
	if !slices.Contains(Formats(), FormatJSON) {
		t.Errorf("Formats() = %q, want json among them", Formats())
	}
}

// TestWriteHTML checks that the page is saved to --out and its path
// printed.
func TestWriteHTML(t *testing.T) {
	t.Parallel()

	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := Flags(fs)
	out := filepath.Join(t.TempDir(), "page.html")
	if err := fs.Parse([]string{"--out", out}); err != nil || opts.Out != out || opts.Open {
		t.Fatalf("Flags parsed %+v (%v), want --out %s", opts, err, out)
	}
	var buf bytes.Buffer
	if err := Write(t.Context(), &buf, &narrate.Module{Name: "Demo"}, FormatHTML, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); err != nil || strings.TrimSpace(buf.String()) != out {
		t.Errorf("page at %s: %v; printed %q", out, err, buf.String())
	}
}
