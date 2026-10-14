// Package markdown renders the module report of package narrate as
// GitHub-flavoured Markdown (SPEC.md 10.3): the change against a base as a
// pull request comment or step summary, and the repository report. It
// reads the report's fields and computes no sentence; it imports narrate
// and the standard library only, never compose.
package markdown
