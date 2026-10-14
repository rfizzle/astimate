// Package narrate defines the module report every renderer of `astimate
// report` shares (SPEC.md 10.3): per package the band, the hotspot score,
// the ceilings, the rebuild, copies and complexity views and the
// plain-language sentences, and, against a base, the change section. The
// sentences are fields, filled by package compose, so renderers only lay
// them out. It imports internal/report only.
package narrate
