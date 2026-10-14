package html

import (
	"bytes"
	"context"
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/rfizzle/astimate/internal/narrate"
)

// DefaultOut is the file `astimate report --format html` writes when no
// --out is given, in the working directory.
const DefaultOut = "astimate-report.html"

// Options say where Save writes the page and whether it opens it.
type Options struct {
	// Out is the file to write; DefaultOut when empty.
	Out string
	// Open opens the page with the platform's opener once written.
	Open bool
}

// Flags registers --out and --open on fs and returns the options they
// set.
func Flags(fs *flag.FlagSet) *Options {
	o := &Options{}
	fs.StringVar(&o.Out, "out", DefaultOut, "with --format html, the file to write")
	fs.BoolVar(&o.Open, "open", false, "with --format html, open the written page in the browser")
	return o
}

// templates are the page, its style sheet and its script.
//
//go:embed templates/*.tmpl
var templates embed.FS

// Write renders m as one self-contained page: the Repository tab and,
// when m has a change, the "This change" tab, with the fonts embedded
// and every chart laid out in advance, so the page loads nothing.
func Write(w io.Writer, m *narrate.Module) error {
	fonts, err := fontFaces()
	if err != nil {
		return err
	}
	return render(w, m, fonts)
}

// render writes the page for m with the @font-face rules fonts.
func render(w io.Writer, m *narrate.Module, fonts template.CSS) error {
	t, err := template.New("page.tmpl").Funcs(template.FuncMap{"num": fmtNum, "int": fmtInt}).
		ParseFS(templates, "templates/*.tmpl")
	if err != nil {
		return fmt.Errorf("parsing the page template: %w", err)
	}
	if err := t.Execute(w, build(m, fonts)); err != nil {
		return fmt.Errorf("writing html module report: %w", err)
	}
	return nil
}

// Save writes the page for m to the file o.Out, DefaultOut when empty,
// and prints its absolute path to stdout; with o.Open it then opens the
// page with the platform's opener.
func Save(ctx context.Context, m *narrate.Module, o Options, stdout io.Writer) error {
	out := o.Out
	if out == "" {
		out = DefaultOut
	}
	var buf bytes.Buffer
	if err := Write(&buf, m); err != nil {
		return err
	}
	abs, err := filepath.Abs(out)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", out, err)
	}
	if err := os.WriteFile(abs, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", abs, err)
	}
	if _, err := fmt.Fprintln(stdout, abs); err != nil {
		return fmt.Errorf("printing the page's path: %w", err)
	}
	if !o.Open {
		return nil
	}
	name, args := opener(runtime.GOOS, abs)
	if err := exec.CommandContext(ctx, name, args...).Run(); err != nil {
		return fmt.Errorf("opening %s with %s: %w", abs, name, err)
	}
	return nil
}

// opener is the command that opens path in the default browser on goos.
func opener(goos, path string) (name string, args []string) {
	switch goos {
	case "darwin":
		return "open", []string{path}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", path}
	}
	return "xdg-open", []string{path}
}
