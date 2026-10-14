// Package render writes the module report (SPEC.md 10.3) in one of its
// formats. It is the one place the formats are listed, so a command
// validates --format against Formats, registers the formats' flags with
// Flags and calls Write. It imports narrate and the renderers under it
// only.
package render

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/narrate/html"
	"github.com/rfizzle/astimate/internal/narrate/markdown"
)

// Format names Write accepts.
const (
	// FormatJSON is narrate.WriteJSON's indented JSON.
	FormatJSON = "json"
	// FormatMarkdown is the Markdown of package markdown.
	FormatMarkdown = "md"
	// FormatHTML is the self-contained page of package html, saved to a
	// file whose path is printed.
	FormatHTML = "html"
)

// Options are Write's options, narrate.WriteOptions, named here so a
// command reaches every format through this package alone.
type Options = narrate.WriteOptions

// Formats returns the formats Write accepts.
func Formats() []string {
	return []string{FormatJSON, FormatMarkdown, FormatHTML}
}

// Flags registers the formats' own flags on fs, --out and --open for the
// page, and returns the options they set.
func Flags(fs *flag.FlagSet) *Options {
	o := &Options{}
	fs.StringVar(&o.Out, "out", html.DefaultOut, "with --format html, the file to write")
	fs.BoolVar(&o.Open, "open", false, "with --format html, open the written page in the browser")
	return o
}

// Write writes m to w in format; each format reads the options it uses.
// The page is saved to opts.Out, and opened with opts.Open, under ctx; w
// gets its path.
func Write(ctx context.Context, w io.Writer, m *narrate.Module, format string, opts *Options) error {
	switch format {
	case FormatJSON:
		return narrate.WriteJSON(w, m)
	case FormatMarkdown:
		return markdown.Write(w, m, markdown.Options{})
	case FormatHTML:
		return html.Save(ctx, m, html.Options{Out: opts.Out, Open: opts.Open}, w)
	}
	return fmt.Errorf("unknown report format %q", format)
}
