// Package compose builds the module report of package narrate from the
// engine's results (Build): the bands, hotspot scores, ceilings, views and
// sentences of SPEC.md 10.3. The rules live in rules.go and every sentence
// in sentences.go, so the terminal, HTML and Markdown renderers say the
// same words; the glossary lives in package narrate (Glossary), which the
// MCP server reads too. It imports narrate, internal/report and
// internal/metrics only, never an extractor, score, gate or baseline.
package compose
