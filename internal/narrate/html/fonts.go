package html

import (
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"strings"
)

// fontFiles are the latin subsets of the page's three typefaces, each
// under the SIL Open Font License beside it (fonts/README).
//
//go:embed fonts/*.woff2
var fontFiles embed.FS

// latin is the unicode-range of Google Fonts' latin subset, which the
// embedded files cover.
const latin = "U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, " +
	"U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD"

// fontFaces returns the @font-face rules of the embedded fonts as data
// URIs, so the page loads nothing; a browser that cannot use them falls
// back to the system stacks the style sheet names after each family.
func fontFaces() (template.CSS, error) {
	faces := []struct{ family, file, weights string }{
		{"Bricolage Grotesque", "bricolage-grotesque-latin.woff2", "200 800"},
		{"DM Sans", "dm-sans-latin.woff2", "100 1000"},
		{"JetBrains Mono", "jetbrains-mono-latin.woff2", "100 800"},
	}
	var b strings.Builder
	for _, f := range faces {
		data, err := fontFiles.ReadFile("fonts/" + f.file)
		if err != nil {
			return "", fmt.Errorf("reading font %s: %w", f.file, err)
		}
		fmt.Fprintf(&b, "@font-face { font-family: %q; font-style: normal; font-weight: %s; font-display: swap; "+
			"src: url(data:font/woff2;base64,%s) format(\"woff2\"); unicode-range: %s }\n",
			f.family, f.weights, base64.StdEncoding.EncodeToString(data), latin)
	}
	// The fonts are the package's own embedded files, so the rules are
	// trusted CSS.
	return template.CSS(b.String()), nil
}
