// Package html renders the module report (package narrate, SPEC.md 10.3)
// as one self-contained HTML page for `astimate report --format html`:
// the report mockup's Repository tab and, with a base, its "This change"
// tab. Nothing is fetched: the fonts are embedded as data URIs, and the
// bubble packing, the scatter's scales and the band ring are computed in
// Go (package layout) and written as SVG through html/template, so the
// page's own script only switches tabs, filters, sorts, opens rows and
// shows tooltips. Every sentence is the report's own field; the page only
// lays them out. It imports narrate, html/template, embed and the
// standard library.
package html
